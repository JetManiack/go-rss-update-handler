package storage

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestListUpdates(t *testing.T) {
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
	inserted, err := store.Updates().InsertNew(ctx, []Update{
		{FeedID: "f1", Fingerprint: "fp1", SourceURL: "u1", PublishedAt: now.Add(-2 * time.Hour), RawContent: &RawContent{Content: "c1"}},
		{FeedID: "f1", Fingerprint: "fp2", SourceURL: "u2", PublishedAt: now.Add(-1 * time.Hour), RawContent: &RawContent{Content: "c2"}},
		{FeedID: "f1", Fingerprint: "fp3", SourceURL: "u3", PublishedAt: now, RawContent: &RawContent{Content: "c3"}},
	})
	if err != nil {
		t.Fatalf("InsertNew: %v", err)
	}
	id := map[string]string{}
	for _, u := range inserted {
		id[u.SourceURL] = u.ID
	}
	if err := store.Updates().SaveVerdict(ctx, id["u1"], Verdict{Important: true, Category: "security", Confidence: 1, Reason: "cve"}); err != nil {
		t.Fatal(err)
	}
	if err := store.Updates().SaveVerdict(ctx, id["u2"], Verdict{Important: false, Category: "noise", Confidence: 0.9, Reason: "minor"}); err != nil {
		t.Fatal(err)
	}
	// u3 is left pending.

	// All: newest published first, content preloaded.
	items, total, err := ListUpdates(ctx, db, "", "", 50, 0)
	if err != nil {
		t.Fatalf("ListUpdates: %v", err)
	}
	if total != 3 || len(items) != 3 {
		t.Fatalf("all: total=%d len=%d, want 3/3", total, len(items))
	}
	if items[0].SourceURL != "u3" {
		t.Errorf("newest first: got %q, want u3", items[0].SourceURL)
	}
	if items[0].RawContent == nil {
		t.Error("raw content not preloaded")
	}

	// Category filter.
	sec, total, _ := ListUpdates(ctx, db, "security", "", 50, 0)
	if total != 1 || len(sec) != 1 || sec[0].SourceURL != "u1" {
		t.Errorf("category=security: total=%d len=%d", total, len(sec))
	}

	// Importance filters.
	if imp, tot, _ := ListUpdates(ctx, db, "", "important", 50, 0); tot != 1 || imp[0].SourceURL != "u1" {
		t.Errorf("importance=important: total=%d", tot)
	}
	if noise, tot, _ := ListUpdates(ctx, db, "", "noise", 50, 0); tot != 1 || noise[0].SourceURL != "u2" {
		t.Errorf("importance=noise: total=%d", tot)
	}
	if pend, tot, _ := ListUpdates(ctx, db, "", "pending", 50, 0); tot != 1 || pend[0].SourceURL != "u3" {
		t.Errorf("importance=pending: total=%d", tot)
	}

	// Pagination keeps the full total but returns one page.
	page1, total, _ := ListUpdates(ctx, db, "", "", 1, 0)
	if total != 3 || len(page1) != 1 || page1[0].SourceURL != "u3" {
		t.Errorf("page1: total=%d len=%d", total, len(page1))
	}
	page2, _, _ := ListUpdates(ctx, db, "", "", 1, 1)
	if len(page2) != 1 || page2[0].SourceURL != "u2" {
		t.Errorf("page2: got %q, want u2", page2[0].SourceURL)
	}
}
