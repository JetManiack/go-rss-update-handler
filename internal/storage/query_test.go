package storage

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestRecentUpdates(t *testing.T) {
	ctx := context.Background()
	store, db, err := InitDB(Config{
		Driver:       "sqlite",
		DSN:          filepath.Join(t.TempDir(), "query.db"),
		MaxOpenConns: 5,
	})
	if err != nil {
		t.Fatalf("InitDB: %v", err)
	}

	now := time.Now()
	updates := []Update{
		{FeedID: "f1", Fingerprint: "fp-old", SourceURL: "u1", PublishedAt: now.Add(-time.Hour), RawContent: &RawContent{Content: "old body"}},
		{FeedID: "f1", Fingerprint: "fp-new", SourceURL: "u2", PublishedAt: now, RawContent: &RawContent{Content: "new body"}},
	}
	if _, err := store.Updates().InsertNew(ctx, updates); err != nil {
		t.Fatalf("InsertNew: %v", err)
	}

	got, err := RecentUpdates(ctx, db, 100)
	if err != nil {
		t.Fatalf("RecentUpdates: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d updates, want 2", len(got))
	}
	// Ordered by publication time, newest first.
	if !got[0].PublishedAt.After(got[1].PublishedAt) {
		t.Errorf("updates must be ordered by published_at DESC: %v then %v", got[0].PublishedAt, got[1].PublishedAt)
	}
	if got[0].SourceURL != "u2" {
		t.Errorf("newest-published update should be first, got %q", got[0].SourceURL)
	}
	for _, u := range got {
		if u.RawContent == nil || u.RawContent.Content == "" {
			t.Errorf("raw content not preloaded for %s", u.SourceURL)
		}
	}

	// limit is respected.
	one, err := RecentUpdates(ctx, db, 1)
	if err != nil {
		t.Fatalf("RecentUpdates(limit=1): %v", err)
	}
	if len(one) != 1 {
		t.Errorf("limit=1 returned %d rows", len(one))
	}
}
