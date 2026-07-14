package storage

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestParseRetention(t *testing.T) {
	cases := []struct {
		in      string
		want    time.Duration
		wantErr bool
	}{
		{"", 0, false},
		{"0", 0, false},
		{"90d", 90 * 24 * time.Hour, false},
		{"2w", 14 * 24 * time.Hour, false},
		{"720h", 720 * time.Hour, false},
		{"30m", 30 * time.Minute, false},
		{"bogus", 0, true},
		{"12x", 0, true},
	}
	for _, c := range cases {
		got, err := ParseRetention(c.in)
		if c.wantErr {
			if err == nil {
				t.Errorf("ParseRetention(%q): expected error, got nil", c.in)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseRetention(%q): unexpected error %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("ParseRetention(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestPurgeRawContent(t *testing.T) {
	ctx := context.Background()
	store, db, err := InitDB(Config{
		Driver:       "sqlite",
		DSN:          filepath.Join(t.TempDir(), "retention.db"),
		MaxOpenConns: 5,
	})
	if err != nil {
		t.Fatalf("InitDB: %v", err)
	}

	now := time.Now()
	updates := []Update{
		{FeedID: "f1", Fingerprint: "fp-old", SourceURL: "u1", PublishedAt: now, RawContent: &RawContent{Content: "old"}},
		{FeedID: "f1", Fingerprint: "fp-new", SourceURL: "u2", PublishedAt: now, RawContent: &RawContent{Content: "new"}},
	}
	if _, err := store.Updates().InsertNew(ctx, updates); err != nil {
		t.Fatalf("InsertNew: %v", err)
	}

	// Backdate the "old" raw content beyond the retention window.
	if err := db.Model(&RawContent{}).Where("content = ?", "old").
		Update("created_at", now.Add(-100*24*time.Hour)).Error; err != nil {
		t.Fatalf("backdate: %v", err)
	}

	removed, err := PurgeRawContent(ctx, db, now.Add(-50*24*time.Hour))
	if err != nil {
		t.Fatalf("PurgeRawContent: %v", err)
	}
	if removed != 1 {
		t.Errorf("removed = %d, want 1", removed)
	}

	var remaining []RawContent
	if err := db.Find(&remaining).Error; err != nil {
		t.Fatalf("find remaining: %v", err)
	}
	if len(remaining) != 1 {
		t.Fatalf("remaining raw_contents = %d, want 1", len(remaining))
	}
	if remaining[0].Content != "new" {
		t.Errorf("remaining content = %q, want %q", remaining[0].Content, "new")
	}

	// updates rows must be untouched (fingerprint/verdict live forever).
	var updateCount int64
	if err := db.Model(&Update{}).Count(&updateCount).Error; err != nil {
		t.Fatalf("count updates: %v", err)
	}
	if updateCount != 2 {
		t.Errorf("updates count = %d, want 2 (retention must not delete updates)", updateCount)
	}
}
