package storage

import (
	"context"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Verdict is the classification result stored alongside the update.
type Verdict struct {
	Important  bool
	Category   string
	Confidence float64
	Reason     string
}

// Store is the unified storage layer interface.
type Store interface {
	Feeds() FeedRepo
	Updates() UpdateRepo
	Channels() ChannelRepo
}

// FeedRepo defines operations on feeds.
type FeedRepo interface {
	List(ctx context.Context) ([]Feed, error)
	UpdateCacheHeaders(ctx context.Context, feedID int64, etag, lastModified string) error
	ChannelsFor(ctx context.Context, feedID int64) ([]string, error)
}

// UpdateRepo defines operations on updates.
type UpdateRepo interface {
	InsertNew(ctx context.Context, updates []Update) ([]Update, error)
	SaveVerdict(ctx context.Context, updateID int64, v Verdict) error
	LastImportant(ctx context.Context, feedID int64, n int) ([]Update, error)
	MarkDispatched(ctx context.Context, updateID int64, channel string) error
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

type feedRepo struct {
	db *gorm.DB
}

func (r *feedRepo) List(ctx context.Context) ([]Feed, error) {
	var feeds []Feed
	err := r.db.WithContext(ctx).Where("active = ?", true).Find(&feeds).Error
	return feeds, err
}

func (r *feedRepo) UpdateCacheHeaders(ctx context.Context, feedID int64, etag, lastModified string) error {
	return r.db.WithContext(ctx).Model(&Feed{}).Where("id = ?", feedID).Updates(map[string]any{
		"etag":          etag,
		"last_modified": lastModified,
	}).Error
}

func (r *feedRepo) ChannelsFor(ctx context.Context, feedID int64) ([]string, error) {
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
		for _, u := range updates {
			// Try to insert update using OnConflict DoNothing
			res := tx.Omit("RawContent").Clauses(clause.OnConflict{
				Columns:   []clause.Column{{Name: "fingerprint"}},
				DoNothing: true,
			}).Create(&u)
			if res.Error != nil {
				return res.Error
			}
			if res.RowsAffected > 0 {
				if u.RawContent != nil {
					u.RawContent.UpdateID = u.ID
					if err := tx.Create(u.RawContent).Error; err != nil {
						return err
					}
				}
				inserted = append(inserted, u)
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

func (r *updateRepo) SaveVerdict(ctx context.Context, updateID int64, v Verdict) error {
	now := time.Now()
	return r.db.WithContext(ctx).Model(&Update{}).Where("id = ?", updateID).Updates(map[string]any{
		"verdict_important":  boolPtr(v.Important),
		"verdict_category":   v.Category,
		"verdict_confidence": v.Confidence,
		"verdict_reason":     v.Reason,
		"classified_at":      &now,
	}).Error
}

func (r *updateRepo) LastImportant(ctx context.Context, feedID int64, n int) ([]Update, error) {
	var updates []Update
	err := r.db.WithContext(ctx).
		Where("feed_id = ? AND verdict_important = ?", feedID, true).
		Order("published_at DESC").
		Limit(n).
		Find(&updates).Error
	return updates, err
}

func (r *updateRepo) MarkDispatched(ctx context.Context, updateID int64, channelName string) error {
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

type channelRepo struct {
	db *gorm.DB
}

func (r *channelRepo) Create(ctx context.Context, ch *Channel) error {
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
