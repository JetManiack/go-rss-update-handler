package storage_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/JetManiack/go-rss-update-handler/internal/storage"
	"gorm.io/gorm"
)

func newPendingStore(t *testing.T) (storage.Store, *gorm.DB) {
	t.Helper()
	store, db, err := storage.InitDB(storage.Config{
		Driver:       "sqlite",
		DSN:          filepath.Join(t.TempDir(), "pending_test.db"),
		MaxOpenConns: 5,
	})
	if err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	return store, db
}

func mkUpdate(id string, pub time.Time) storage.Update {
	return storage.Update{
		ID:          id,
		FeedID:      "feed-1",
		Fingerprint: id,
		Title:       id,
		PublishedAt: pub,
		RawContent:  &storage.RawContent{Content: "body-" + id},
	}
}

func TestUpdateRepo_ListPending_ReturnsUnclassifiedOldestFirst(t *testing.T) {
	ctx := t.Context()
	store, _ := newPendingStore(t)
	repo := store.Updates()

	base := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	if _, err := repo.InsertNew(ctx, []storage.Update{
		mkUpdate("u-new", base.Add(48*time.Hour)),
		mkUpdate("u-old", base),
		mkUpdate("u-mid", base.Add(24*time.Hour)),
	}); err != nil {
		t.Fatalf("InsertNew: %v", err)
	}

	// Classifying one update must drop it out of the pending set.
	if err := repo.SaveVerdict(ctx, "u-mid", storage.Verdict{Category: "noise", Confidence: 0.5, Reason: "x"}); err != nil {
		t.Fatalf("SaveVerdict: %v", err)
	}

	pending, err := repo.ListPending(ctx, 10)
	if err != nil {
		t.Fatalf("ListPending: %v", err)
	}
	if len(pending) != 2 {
		t.Fatalf("expected 2 pending, got %d", len(pending))
	}
	if pending[0].ID != "u-old" || pending[1].ID != "u-new" {
		t.Errorf("expected oldest-first [u-old, u-new], got [%s, %s]", pending[0].ID, pending[1].ID)
	}
	if pending[0].RawContent == nil || pending[0].RawContent.Content != "body-u-old" {
		t.Errorf("RawContent not preloaded: %+v", pending[0].RawContent)
	}
}

func TestUpdateRepo_ListUndispatchedImportant(t *testing.T) {
	ctx := t.Context()
	store, db := newPendingStore(t)
	repo := store.Updates()

	feed := storage.Feed{ID: "feed-1", URL: "http://e/f", Active: true, CreatedAt: time.Now()}
	if err := db.Create(&feed).Error; err != nil {
		t.Fatalf("seed feed: %v", err)
	}
	ch := storage.Channel{ID: "ch-1", Name: "c1", Type: "webhook", ConfigJSON: "{}"}
	if err := db.Create(&ch).Error; err != nil {
		t.Fatalf("seed channel: %v", err)
	}
	if err := db.Exec("INSERT INTO feed_channels (feed_id, channel_id) VALUES (?, ?)", feed.ID, ch.ID).Error; err != nil {
		t.Fatalf("seed mapping: %v", err)
	}

	base := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	if _, err := repo.InsertNew(ctx, []storage.Update{
		mkUpdate("imp-undel", base),              // important, classified, NOT dispatched -> expected
		mkUpdate("imp-del", base.Add(time.Hour)), // important, classified, dispatched -> excluded
		mkUpdate("noise", base.Add(2*time.Hour)), // classified noise -> excluded
		mkUpdate("pend", base.Add(3*time.Hour)),  // unclassified -> excluded
	}); err != nil {
		t.Fatalf("InsertNew: %v", err)
	}
	imp := storage.Verdict{Important: true, Category: "release", Confidence: 0.9, Reason: "x"}
	if err := repo.SaveVerdict(ctx, "imp-undel", imp); err != nil {
		t.Fatalf("SaveVerdict imp-undel: %v", err)
	}
	if err := repo.SaveVerdict(ctx, "imp-del", imp); err != nil {
		t.Fatalf("SaveVerdict imp-del: %v", err)
	}
	if err := repo.SaveVerdict(ctx, "noise", storage.Verdict{Category: "noise", Confidence: 0.5, Reason: "x"}); err != nil {
		t.Fatalf("SaveVerdict noise: %v", err)
	}
	if err := repo.MarkDispatched(ctx, "imp-del", "c1"); err != nil {
		t.Fatalf("MarkDispatched: %v", err)
	}

	got, err := repo.ListUndispatchedImportant(ctx, 10)
	if err != nil {
		t.Fatalf("ListUndispatchedImportant: %v", err)
	}
	if len(got) != 1 || got[0].ID != "imp-undel" {
		ids := make([]string, len(got))
		for i, u := range got {
			ids[i] = u.ID
		}
		t.Fatalf("expected [imp-undel], got %v", ids)
	}
	if got[0].RawContent == nil || got[0].RawContent.Content != "body-imp-undel" {
		t.Errorf("RawContent not preloaded: %+v", got[0].RawContent)
	}
}
