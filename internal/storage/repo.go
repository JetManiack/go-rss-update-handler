package storage

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Verdict is the classification result stored alongside the update.
type Verdict struct {
	Important  bool
	Category   string
	Confidence float64
	Reason     string
	Title      string // LLM-generated short title (project, version, summary)
}

// Store is the unified storage layer interface.
type Store interface {
	Feeds() FeedRepo
	Updates() UpdateRepo
	Channels() ChannelRepo
	// Close flushes pending writes to the primary database file and releases
	// the connection pool. It must be called on graceful shutdown.
	Close() error
}

// FeedRepo defines operations on feeds.
type FeedRepo interface {
	List(ctx context.Context) ([]Feed, error)
	UpdateCacheHeaders(ctx context.Context, feedID string, etag, lastModified string) error
	ChannelsFor(ctx context.Context, feedID string) ([]string, error)
}

// UpdateRepo defines operations on updates.
type UpdateRepo interface {
	InsertNew(ctx context.Context, updates []Update) ([]Update, error)
	SaveVerdict(ctx context.Context, updateID string, v Verdict) error
	GetVerdict(ctx context.Context, updateID string) (Verdict, error)
	LastImportant(ctx context.Context, feedID string, n int) ([]Update, error)
	MarkDispatched(ctx context.Context, updateID string, channel string) error
	IsDispatched(ctx context.Context, updateID string, channel string) (bool, error)
	// ListPending returns updates that have not been classified yet
	// (classified_at IS NULL), oldest-first, with raw content preloaded.
	ListPending(ctx context.Context, limit int) ([]Update, error)
	// ListUndispatchedImportant returns classified-important updates that still
	// have at least one channel they were never delivered to, oldest-first, with
	// raw content preloaded.
	ListUndispatchedImportant(ctx context.Context, limit int) ([]Update, error)
}

// ChannelRepo defines operations on channels.
type ChannelRepo interface {
	Create(ctx context.Context, ch *Channel) error
	FindByName(ctx context.Context, name string) (*Channel, error)
}

// gormStore implements Store using GORM.
type gormStore struct {
	db *gorm.DB
}

// NewStore creates a new GORM-backed Store.
func NewStore(db *gorm.DB) Store {
	return &gormStore{db: db}
}

func (s *gormStore) Feeds() FeedRepo {
	return &feedRepo{db: s.db}
}

func (s *gormStore) Updates() UpdateRepo {
	return &updateRepo{db: s.db}
}

func (s *gormStore) Channels() ChannelRepo {
	return &channelRepo{db: s.db}
}

// Close flushes pending writes to the primary database file and releases the
// connection pool.
//
// For sqlite in WAL mode it first checkpoints the write-ahead log with TRUNCATE
// so all committed data is folded back into the main .db file; otherwise the
// data lives only in the -wal sidecar and is lost if the file is copied or
// moved on its own. Closing the pool then lets sqlite remove the -wal/-shm
// sidecars, leaving a self-contained, portable database file.
func (s *gormStore) Close() error {
	sqlDB, err := s.db.DB()
	if err != nil {
		return err
	}
	if s.db.Dialector.Name() == "sqlite" {
		if err := s.db.Exec("PRAGMA wal_checkpoint(TRUNCATE);").Error; err != nil {
			return fmt.Errorf("storage: wal checkpoint on close: %w", err)
		}
	}
	return sqlDB.Close()
}

type feedRepo struct {
	db *gorm.DB
}

func (r *feedRepo) List(ctx context.Context) ([]Feed, error) {
	var feeds []Feed
	err := r.db.WithContext(ctx).Where("active = ?", true).Find(&feeds).Error
	return feeds, err
}

func (r *feedRepo) UpdateCacheHeaders(ctx context.Context, feedID string, etag, lastModified string) error {
	return r.db.WithContext(ctx).Model(&Feed{}).Where("id = ?", feedID).Updates(map[string]any{
		"etag":          etag,
		"last_modified": lastModified,
	}).Error
}

func (r *feedRepo) ChannelsFor(ctx context.Context, feedID string) ([]string, error) {
	var names []string
	err := r.db.WithContext(ctx).Table("channels").
		Joins("join feed_channels on channels.id = feed_channels.channel_id").
		Where("feed_channels.feed_id = ?", feedID).
		Pluck("name", &names).Error
	return names, err
}

type updateRepo struct {
	db *gorm.DB
}

func (r *updateRepo) InsertNew(ctx context.Context, updates []Update) ([]Update, error) {
	var inserted []Update
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for i := range updates {
			if updates[i].ID == "" {
				updates[i].ID = uuid.NewString()
			}
			// Try to insert update using OnConflict DoNothing
			res := tx.Omit("RawContent").Clauses(clause.OnConflict{
				Columns:   []clause.Column{{Name: "fingerprint"}},
				DoNothing: true,
			}).Create(&updates[i])
			if res.Error != nil {
				return res.Error
			}
			if res.RowsAffected > 0 {
				if updates[i].RawContent != nil {
					updates[i].RawContent.UpdateID = updates[i].ID
					if err := tx.Create(updates[i].RawContent).Error; err != nil {
						return err
					}
				}
				inserted = append(inserted, updates[i])
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return inserted, nil
}

func boolPtr(b bool) *bool {
	return &b
}

func (r *updateRepo) SaveVerdict(ctx context.Context, updateID string, v Verdict) error {
	now := time.Now()
	fields := map[string]any{
		"verdict_important":  boolPtr(v.Important),
		"verdict_category":   v.Category,
		"verdict_confidence": v.Confidence,
		"verdict_reason":     v.Reason,
		"classified_at":      &now,
	}
	// Replace the feed title with the richer LLM-generated one when present.
	if v.Title != "" {
		fields["title"] = v.Title
	}
	return r.db.WithContext(ctx).Model(&Update{}).Where("id = ?", updateID).Updates(fields).Error
}

func (r *updateRepo) GetVerdict(ctx context.Context, updateID string) (Verdict, error) {
	var u Update
	err := r.db.WithContext(ctx).Model(&Update{}).Where("id = ?", updateID).First(&u).Error
	if err != nil {
		return Verdict{}, err
	}
	return Verdict{
		Important:  *u.VerdictImportant,
		Category:   u.VerdictCategory,
		Confidence: u.VerdictConfidence,
		Reason:     u.VerdictReason,
	}, nil
}

func (r *updateRepo) LastImportant(ctx context.Context, feedID string, n int) ([]Update, error) {
	var updates []Update
	err := r.db.WithContext(ctx).
		Where("feed_id = ? AND verdict_important = ?", feedID, true).
		Order("published_at DESC").
		Limit(n).
		Find(&updates).Error
	return updates, err
}

func (r *updateRepo) IsDispatched(ctx context.Context, updateID string, channelName string) (bool, error) {
	var ch Channel
	if err := r.db.WithContext(ctx).Where("name = ?", channelName).First(&ch).Error; err != nil {
		return false, err
	}
	var count int64
	err := r.db.WithContext(ctx).Model(&Dispatch{}).Where("update_id = ? AND channel_id = ?", updateID, ch.ID).Count(&count).Error
	return count > 0, err
}

func (r *updateRepo) MarkDispatched(ctx context.Context, updateID string, channelName string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var ch Channel
		if err := tx.Where("name = ?", channelName).First(&ch).Error; err != nil {
			return err
		}
		dispatch := Dispatch{
			UpdateID:    updateID,
			ChannelID:   ch.ID,
			DeliveredAt: time.Now(),
		}
		return tx.Clauses(clause.OnConflict{
			DoNothing: true,
		}).Create(&dispatch).Error
	})
}

// defaultReconcileLimit caps how many rows a single reconcile query returns
// when the caller passes a non-positive limit.
const defaultReconcileLimit = 500

func (r *updateRepo) ListPending(ctx context.Context, limit int) ([]Update, error) {
	if limit <= 0 {
		limit = defaultReconcileLimit
	}
	var updates []Update
	err := r.db.WithContext(ctx).
		Where("classified_at IS NULL").
		Preload("RawContent").
		Order("published_at ASC, created_at ASC").
		Limit(limit).
		Find(&updates).Error
	return updates, err
}

func (r *updateRepo) ListUndispatchedImportant(ctx context.Context, limit int) ([]Update, error) {
	if limit <= 0 {
		limit = defaultReconcileLimit
	}
	var updates []Update
	// An important update still needs delivery while its feed maps to more
	// channels than it has dispatch rows. Fully-delivered updates (and updates
	// whose feed has no channels) are excluded, so this does not re-publish work
	// that is already done on every tick.
	err := r.db.WithContext(ctx).
		Where("verdict_important = ? AND classified_at IS NOT NULL", true).
		Where(`(SELECT COUNT(*) FROM feed_channels fc WHERE fc.feed_id = updates.feed_id) >
		        (SELECT COUNT(*) FROM dispatches d WHERE d.update_id = updates.id)`).
		Preload("RawContent").
		Order("published_at ASC, created_at ASC").
		Limit(limit).
		Find(&updates).Error
	return updates, err
}

type channelRepo struct {
	db *gorm.DB
}

func (r *channelRepo) Create(ctx context.Context, ch *Channel) error {
	if ch.ID == "" {
		ch.ID = uuid.NewString()
	}
	return r.db.WithContext(ctx).Create(ch).Error
}

func (r *channelRepo) FindByName(ctx context.Context, name string) (*Channel, error) {
	var ch Channel
	err := r.db.WithContext(ctx).Where("name = ?", name).First(&ch).Error
	if err != nil {
		return nil, err
	}
	return &ch, nil
}
