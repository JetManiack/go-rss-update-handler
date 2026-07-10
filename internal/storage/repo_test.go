package storage_test

import (
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"go-rss-update-handler/internal/storage"
)

func TestStorage_Flow(t *testing.T) {
	ctx := t.Context()

	// Initialize sqlite file-based DB in a temp directory
	dbPath := filepath.Join(t.TempDir(), "test.db")
	cfg := storage.Config{
		Driver:       "sqlite",
		DSN:          dbPath,
		MaxOpenConns: 5,
	}

	store, db, err := storage.InitDB(cfg)
	if err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}

	// 1. Setup Feeds and Channels
	feed1 := storage.Feed{
		URL:       "https://example.com/feed1",
		Active:    true,
		CreatedAt: time.Now(),
	}
	if err := db.Create(&feed1).Error; err != nil {
		t.Fatalf("Failed to create feed: %v", err)
	}

	channel1 := storage.Channel{
		Name:       "telegram_chan",
		Type:       "telegram",
		ConfigJSON: `{"chat_id": "123"}`,
	}
	if err := store.Channels().Create(ctx, &channel1); err != nil {
		t.Fatalf("Failed to create channel: %v", err)
	}

	// Link feed to channel via association
	if err := db.Model(&feed1).Association("Channels").Append(&channel1); err != nil {
		t.Fatalf("Failed to link feed to channel: %v", err)
	}

	// 2. Test FeedRepo List
	feeds, err := store.Feeds().List(ctx)
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(feeds) != 1 || feeds[0].URL != feed1.URL {
		t.Errorf("List() returned %v, want [%v]", feeds, feed1)
	}

	// 3. Test FeedRepo UpdateCacheHeaders
	err = store.Feeds().UpdateCacheHeaders(ctx, feed1.ID, "etag123", "Mon, 01 Jan 2026 00:00:00 GMT")
	if err != nil {
		t.Fatalf("UpdateCacheHeaders failed: %v", err)
	}
	var updatedFeed storage.Feed
	if err := db.First(&updatedFeed, feed1.ID).Error; err != nil {
		t.Fatalf("Failed to fetch updated feed: %v", err)
	}
	if updatedFeed.Etag != "etag123" || updatedFeed.LastModified != "Mon, 01 Jan 2026 00:00:00 GMT" {
		t.Errorf("Etag or LastModified not updated: %v", updatedFeed)
	}

	// 4. Test FeedRepo ChannelsFor
	chNames, err := store.Feeds().ChannelsFor(ctx, feed1.ID)
	if err != nil {
		t.Fatalf("ChannelsFor failed: %v", err)
	}
	if len(chNames) != 1 || chNames[0] != "telegram_chan" {
		t.Errorf("ChannelsFor() returned %v, want [telegram_chan]", chNames)
	}

	// 5. Test UpdateRepo InsertNew (and deduplication)
	updates := []storage.Update{
		{
			FeedID:      feed1.ID,
			Fingerprint: "fp1",
			SourceURL:   "https://example.com/item1",
			PublishedAt: time.Now().Add(-2 * time.Hour),
			CreatedAt:   time.Now(),
			RawContent: &storage.RawContent{
				Content:   "some raw content 1",
				CreatedAt: time.Now(),
			},
		},
		{
			FeedID:      feed1.ID,
			Fingerprint: "fp2",
			SourceURL:   "https://example.com/item2",
			PublishedAt: time.Now().Add(-1 * time.Hour),
			CreatedAt:   time.Now(),
			RawContent: &storage.RawContent{
				Content:   "some raw content 2",
				CreatedAt: time.Now(),
			},
		},
	}

	inserted, err := store.Updates().InsertNew(ctx, updates)
	if err != nil {
		t.Fatalf("InsertNew failed: %v", err)
	}
	if len(inserted) != 2 {
		t.Errorf("InsertNew inserted %d updates, want 2", len(inserted))
	}

	// Attempt inserting again (should be deduplicated)
	insertedAgain, err := store.Updates().InsertNew(ctx, updates)
	if err != nil {
		t.Fatalf("InsertNew on duplicates failed: %v", err)
	}
	if len(insertedAgain) != 0 {
		t.Errorf("InsertNew on duplicates inserted %d updates, want 0", len(insertedAgain))
	}

	// 6. Test Concurrent InsertNew
	var wg sync.WaitGroup
	concurrentCount := 5
	errChan := make(chan error, concurrentCount)

	for i := range concurrentCount {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			// Generate one unique and one duplicate update
			concurrentUpdates := []storage.Update{
				{
					FeedID:      feed1.ID,
					Fingerprint: "fp2", // duplicate
					SourceURL:   "https://example.com/item2",
					PublishedAt: time.Now(),
					CreatedAt:   time.Now(),
				},
				{
					FeedID:      feed1.ID,
					Fingerprint: fmt.Sprintf("fp_concurrent_%d", idx), // unique
					SourceURL:   "https://example.com/item_concurrent",
					PublishedAt: time.Now(),
					CreatedAt:   time.Now(),
				},
			}
			_, cErr := store.Updates().InsertNew(ctx, concurrentUpdates)
			if cErr != nil {
				errChan <- cErr
			}
		}(i)
	}
	wg.Wait()
	close(errChan)

	for cErr := range errChan {
		t.Errorf("Concurrent InsertNew failed: %v", cErr)
	}

	// 7. Test SaveVerdict and LastImportant
	v1 := storage.Verdict{
		Important:  true,
		Category:   "major_release",
		Confidence: 0.9,
		Reason:     "Major bump found",
	}
	err = store.Updates().SaveVerdict(ctx, inserted[0].ID, v1)
	if err != nil {
		t.Fatalf("SaveVerdict failed: %v", err)
	}

	v2 := storage.Verdict{
		Important:  false,
		Category:   "noise",
		Confidence: 0.1,
		Reason:     "No key info",
	}
	err = store.Updates().SaveVerdict(ctx, inserted[1].ID, v2)
	if err != nil {
		t.Fatalf("SaveVerdict failed: %v", err)
	}

	important, err := store.Updates().LastImportant(ctx, feed1.ID, 5)
	if err != nil {
		t.Fatalf("LastImportant failed: %v", err)
	}
	if len(important) != 1 {
		t.Errorf("LastImportant returned %d items, want 1", len(important))
	} else {
		if important[0].ID != inserted[0].ID {
			t.Errorf("LastImportant returned update ID %d, want %d", important[0].ID, inserted[0].ID)
		}
		if important[0].VerdictImportant == nil || !*important[0].VerdictImportant {
			t.Errorf("VerdictImportant is not true: %v", important[0].VerdictImportant)
		}
	}

	// 8. Test MarkDispatched (idempotency)
	err = store.Updates().MarkDispatched(ctx, inserted[0].ID, "telegram_chan")
	if err != nil {
		t.Fatalf("MarkDispatched failed: %v", err)
	}

	// Duplicate dispatch check
	err = store.Updates().MarkDispatched(ctx, inserted[0].ID, "telegram_chan")
	if err != nil {
		t.Fatalf("MarkDispatched duplicate failed: %v", err)
	}

	var dispatchCount int64
	err = db.Model(&storage.Dispatch{}).
		Where("update_id = ? AND channel_id = ?", inserted[0].ID, channel1.ID).
		Count(&dispatchCount).Error
	if err != nil {
		t.Fatalf("Failed to count dispatches: %v", err)
	}
	if dispatchCount != 1 {
		t.Errorf("Dispatch count is %d, want 1 (idempotent check failed)", dispatchCount)
	}
}
