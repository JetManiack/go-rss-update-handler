package storage_test

import (
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/JetManiack/go-rss-update-handler/internal/storage"
)

// TestStore_Close_CheckpointsWAL reproduces the shutdown bug: without a clean
// close, sqlite leaves all writes in the -wal file and never folds them into
// the main .db file. Moving/copying just the .db file then loses the data.
//
// After Close(), the main .db file alone (no -wal/-shm alongside) must be
// self-contained and readable.
func TestStore_Close_CheckpointsWAL(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "gruh.db")
	cfg := storage.Config{Driver: "sqlite", DSN: dbPath, MaxOpenConns: 5}

	store, db, err := storage.InitDB(cfg)
	if err != nil {
		t.Fatalf("InitDB: %v", err)
	}

	feed := storage.Feed{
		ID:        uuid.NewString(),
		URL:       "https://example.com/feed",
		Active:    true,
		CreatedAt: time.Now(),
	}
	if err := db.Create(&feed).Error; err != nil {
		t.Fatalf("create feed: %v", err)
	}

	// Clean shutdown: must checkpoint the WAL and release the file.
	if err := store.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Simulate "moved the db to another folder" — copy ONLY the main file,
	// leaving -wal and -shm behind.
	movedDir := t.TempDir()
	movedPath := filepath.Join(movedDir, "gruh.db")
	copyFile(t, dbPath, movedPath)

	store2, _, err := storage.InitDB(storage.Config{Driver: "sqlite", DSN: movedPath, MaxOpenConns: 5})
	if err != nil {
		t.Fatalf("reopen moved db: %v", err)
	}
	defer func() { _ = store2.Close() }()

	feeds, err := store2.Feeds().List(t.Context())
	if err != nil {
		t.Fatalf("list feeds from moved db: %v", err)
	}
	if len(feeds) != 1 {
		t.Fatalf("expected 1 feed in moved db file, got %d (data stranded in -wal)", len(feeds))
	}
}

func copyFile(t *testing.T, src, dst string) {
	t.Helper()
	in, err := os.Open(src) // #nosec G304 -- test paths
	if err != nil {
		t.Fatalf("open src: %v", err)
	}
	defer func() { _ = in.Close() }()
	out, err := os.Create(dst) // #nosec G304 -- test paths
	if err != nil {
		t.Fatalf("create dst: %v", err)
	}
	defer func() { _ = out.Close() }()
	if _, err := io.Copy(out, in); err != nil {
		t.Fatalf("copy: %v", err)
	}
}
