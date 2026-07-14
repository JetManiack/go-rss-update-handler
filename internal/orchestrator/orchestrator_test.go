package orchestrator

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/jetbrains/go-rss-update-handler/internal/bus"
	"github.com/jetbrains/go-rss-update-handler/internal/classificator"
	"github.com/jetbrains/go-rss-update-handler/internal/collector"
	"github.com/jetbrains/go-rss-update-handler/internal/deduplicator"
	"github.com/jetbrains/go-rss-update-handler/internal/dispatcher"
	"github.com/jetbrains/go-rss-update-handler/internal/llm"
	"github.com/jetbrains/go-rss-update-handler/internal/parser"
	"github.com/jetbrains/go-rss-update-handler/internal/storage"
	"gorm.io/gorm"
)

// fakeLLM is a test double that always classifies the update as important,
// used only to isolate the real LLM API call in this integration test.
type fakeLLM struct{}

func (f *fakeLLM) Complete(_ context.Context, _ llm.Request) (llm.Response, error) {
	return llm.Response{Content: `{"title": "NodeExporter v1 — test release", "important": true, "category": "release", "confidence": 0.8, "reason": "test"}`}, nil
}

// fakePrompts is a test double avoiding disk/registry dependencies for the prompt template.
type fakePrompts struct{}

func (f *fakePrompts) Execute(_ context.Context, _ string, _ any) (string, string, error) {
	return "system", "user", nil
}

const feedXML = `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0">
  <channel>
    <item>
      <title>Item 1</title>
      <link>http://example.com/1</link>
      <pubDate>Wed, 21 Oct 2025 07:28:00 GMT</pubDate>
    </item>
  </channel>
</rss>`

type testFixture struct {
	orch  *Orchestrator
	store storage.Store
	db    *gorm.DB
	feed  storage.Feed
}

func newTestFixture(t *testing.T) *testFixture {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("If-None-Match") == "etag-cached" {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", "etag-new")
		w.Header().Set("Last-Modified", "Wed, 21 Oct 2025 07:28:00 GMT")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(feedXML))
	}))
	t.Cleanup(server.Close)

	dbPath := filepath.Join(t.TempDir(), "orchestrator_test.db")
	store, db, err := storage.InitDB(storage.Config{
		Driver:       "sqlite",
		DSN:          dbPath,
		MaxOpenConns: 5,
	})
	if err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}

	feed := storage.Feed{
		ID:        "feed-1",
		URL:       server.URL,
		Active:    true,
		CreatedAt: time.Now(),
	}
	if err := db.Create(&feed).Error; err != nil {
		t.Fatalf("failed to seed feed: %v", err)
	}

	c := collector.NewCollector(collector.Config{
		Timeout:     time.Second,
		RatePerHost: 100,
		Retries:     1,
		BackoffBase: time.Millisecond,
		UserAgent:   "test",
	})
	p := parser.NewParser()
	d := deduplicator.NewDeduplicator()
	b := bus.NewMemoryBus()
	classifierSvc := classificator.New(&fakeLLM{}, &fakePrompts{}, classificator.Config{ConfidenceThreshold: 0.5, MaxFormatRetries: 2})

	orch := NewOrchestrator(c, p, d, b, store.Feeds(), store.Updates(), classifierSvc, &dispatcher.Service{Notifiers: make(map[string]dispatcher.Notifier)}, slog.Default())

	return &testFixture{orch: orch, store: store, db: db, feed: feed}
}

func TestOrchestrator_ProcessFeed_PersistsUpdatesAndSkipsDuplicates(t *testing.T) {
	f := newTestFixture(t)
	ctx := t.Context()

	if err := f.orch.ProcessFeed(ctx, f.feed); err != nil {
		t.Fatalf("ProcessFeed failed: %v", err)
	}

	var count int64
	if err := f.db.Model(&storage.Update{}).Where("feed_id = ?", f.feed.ID).Count(&count).Error; err != nil {
		t.Fatalf("failed to count updates: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 update after first ProcessFeed, got %d", count)
	}

	// Re-processing the same feed content must not create duplicate updates,
	// since the fingerprint-based deduplication should skip the item.
	if err := f.orch.ProcessFeed(ctx, f.feed); err != nil {
		t.Fatalf("second ProcessFeed failed: %v", err)
	}
	if err := f.db.Model(&storage.Update{}).Where("feed_id = ?", f.feed.ID).Count(&count).Error; err != nil {
		t.Fatalf("failed to count updates after second run: %v", err)
	}
	if count != 1 {
		t.Errorf("expected still 1 update after duplicate ProcessFeed, got %d", count)
	}
}

func TestOrchestrator_ProcessFeed_UpdatesCacheHeaders(t *testing.T) {
	f := newTestFixture(t)
	ctx := t.Context()

	if err := f.orch.ProcessFeed(ctx, f.feed); err != nil {
		t.Fatalf("ProcessFeed failed: %v", err)
	}

	var updatedFeed storage.Feed
	if err := f.db.First(&updatedFeed, "id = ?", f.feed.ID).Error; err != nil {
		t.Fatalf("failed to fetch feed: %v", err)
	}
	if updatedFeed.Etag != "etag-new" {
		t.Errorf("expected ETag to be persisted as 'etag-new', got %q", updatedFeed.Etag)
	}
	if updatedFeed.LastModified != "Wed, 21 Oct 2025 07:28:00 GMT" {
		t.Errorf("expected LastModified to be persisted, got %q", updatedFeed.LastModified)
	}
}

func TestOrchestrator_RunWorker_ClassifiesAndSavesVerdict(t *testing.T) {
	f := newTestFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	go func() {
		_ = f.orch.RunWorker(ctx)
	}()
	time.Sleep(20 * time.Millisecond) // let the subscription register before publishing

	if err := f.orch.ProcessFeed(ctx, f.feed); err != nil {
		t.Fatalf("ProcessFeed failed: %v", err)
	}

	// The in-memory bus delivers asynchronously, so poll until the worker has
	// classified the update and persisted its verdict.
	var (
		important []storage.Update
		err       error
	)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		important, err = f.store.Updates().LastImportant(ctx, f.feed.ID, 10)
		if err != nil {
			t.Fatalf("LastImportant failed: %v", err)
		}
		if len(important) == 1 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if len(important) != 1 {
		t.Fatalf("expected 1 important update after classification, got %d", len(important))
	}
	if important[0].VerdictCategory != "release" {
		t.Errorf("expected category 'release', got %q", important[0].VerdictCategory)
	}
	if important[0].Title != "NodeExporter v1 — test release" {
		t.Errorf("LLM-generated title not applied to the update: %q", important[0].Title)
	}
	if important[0].VerdictConfidence != 0.8 {
		t.Errorf("expected confidence 0.8, got %v", important[0].VerdictConfidence)
	}
}
