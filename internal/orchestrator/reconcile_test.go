package orchestrator

import (
	"context"
	"log/slog"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/JetManiack/go-rss-update-handler/internal/bus"
	"github.com/JetManiack/go-rss-update-handler/internal/storage"
)

// topicRecordingBus records the update IDs published to each topic.
type topicRecordingBus struct {
	mu      sync.Mutex
	byTopic map[string][]string
}

func newTopicRecordingBus() *topicRecordingBus {
	return &topicRecordingBus{byTopic: make(map[string][]string)}
}

func (b *topicRecordingBus) Publish(_ context.Context, topic string, msg bus.Message) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.byTopic[topic] = append(b.byTopic[topic], msg.Event.ID)
	return nil
}

func (b *topicRecordingBus) Subscribe(ctx context.Context, _, _ string, _ bus.Handler) error {
	<-ctx.Done()
	return ctx.Err()
}

// ReconcilePending must re-publish unclassified updates to the classification
// topic and important-but-undispatched updates to the delivery topic, so a
// restart resumes work that the in-memory bus lost.
func TestOrchestrator_ReconcilePending(t *testing.T) {
	ctx := t.Context()
	store, db, err := storage.InitDB(storage.Config{
		Driver:       "sqlite",
		DSN:          filepath.Join(t.TempDir(), "reconcile_test.db"),
		MaxOpenConns: 5,
	})
	if err != nil {
		t.Fatalf("InitDB: %v", err)
	}

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

	repo := store.Updates()
	base := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	mk := func(id string, pub time.Time) storage.Update {
		return storage.Update{ID: id, FeedID: "feed-1", Fingerprint: id, Title: id, PublishedAt: pub, RawContent: &storage.RawContent{Content: "b-" + id}}
	}
	if _, err := repo.InsertNew(ctx, []storage.Update{
		mk("pending-1", base),
		mk("imp-undel", base.Add(time.Hour)),
	}); err != nil {
		t.Fatalf("InsertNew: %v", err)
	}
	// imp-undel is classified important (so it's no longer pending), but never dispatched.
	if err := repo.SaveVerdict(ctx, "imp-undel", storage.Verdict{Important: true, Category: "release", Confidence: 0.9, Reason: "x"}); err != nil {
		t.Fatalf("SaveVerdict: %v", err)
	}

	rb := newTopicRecordingBus()
	orch := NewOrchestrator(nil, nil, nil, rb, store.Feeds(), repo, nil, nil, slog.Default())

	if err := orch.ReconcilePending(ctx); err != nil {
		t.Fatalf("ReconcilePending: %v", err)
	}

	if got := rb.byTopic[bus.TopicUpdatesNew]; len(got) != 1 || got[0] != "pending-1" {
		t.Errorf("TopicUpdatesNew: want [pending-1], got %v", got)
	}
	if got := rb.byTopic[bus.TopicUpdatesImportant]; len(got) != 1 || got[0] != "imp-undel" {
		t.Errorf("TopicUpdatesImportant: want [imp-undel], got %v", got)
	}
}

// End-to-end restart scenario: an update persisted but never classified (the
// in-memory bus lost its event on shutdown) must be picked up and classified
// once the worker is running and ReconcilePending fires.
func TestOrchestrator_ReconcilePending_ResumesClassificationAfterRestart(t *testing.T) {
	f := newTestFixture(t) // fakeLLM (important), real MemoryBus, real store
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	// Insert directly, leaving classified_at NULL and NOT publishing — this is
	// exactly the state a leftover update is in after a restart.
	if _, err := f.store.Updates().InsertNew(ctx, []storage.Update{{
		ID:          "leftover",
		FeedID:      f.feed.ID,
		Fingerprint: "leftover-fp",
		Title:       "leftover",
		SourceURL:   "http://e/leftover",
		PublishedAt: time.Now().Add(-time.Hour),
		RawContent:  &storage.RawContent{Content: "body"},
	}}); err != nil {
		t.Fatalf("seed pending: %v", err)
	}

	// Mirror run() ordering: worker subscribes first, then reconcile publishes.
	go func() { _ = f.orch.RunWorker(ctx) }()
	time.Sleep(50 * time.Millisecond) // let the subscription register

	if err := f.orch.ReconcilePending(ctx); err != nil {
		t.Fatalf("ReconcilePending: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		var u storage.Update
		if err := f.db.First(&u, "id = ?", "leftover").Error; err != nil {
			t.Fatalf("fetch leftover: %v", err)
		}
		if u.ClassifiedAt != nil {
			return // classified — the pipeline resumed
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("leftover update was never classified after ReconcilePending")
}
