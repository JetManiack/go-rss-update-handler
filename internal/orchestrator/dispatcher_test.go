package orchestrator

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jetbrains/go-rss-update-handler/internal/dispatcher"
	"github.com/jetbrains/go-rss-update-handler/internal/storage"
)

type mockNotifier struct {
	sent atomic.Bool
}

func (m *mockNotifier) Name() string { return "test" }
func (m *mockNotifier) Send(_ context.Context, _ dispatcher.Notification) error {
	m.sent.Store(true)
	return nil
}

// End-to-end: the worker classifies and publishes important updates, and the
// dispatcher role delivers them to the mapped channel.
func TestOrchestrator_WorkerThenDispatcher_Delivers(t *testing.T) {
	f := newTestFixture(t)
	mock := &mockNotifier{}
	f.orch.dispatcher = &dispatcher.Service{Notifiers: map[string]dispatcher.Notifier{"test": mock}}

	// Seed channel and feed→channel mapping.
	ch := storage.Channel{ID: "ch-1", Name: "test", Type: "webhook", ConfigJSON: "{}"}
	if err := f.db.Create(&ch).Error; err != nil {
		t.Fatalf("seed channel: %v", err)
	}
	if err := f.db.Exec("INSERT INTO feed_channels (feed_id, channel_id) VALUES (?, ?)", f.feed.ID, ch.ID).Error; err != nil {
		t.Fatalf("seed mapping: %v", err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	go func() { _ = f.orch.RunWorker(ctx) }()
	go func() { _ = f.orch.RunDispatcher(ctx) }()
	time.Sleep(50 * time.Millisecond) // let subscriptions register

	if err := f.orch.ProcessFeed(ctx, f.feed); err != nil {
		t.Fatalf("ProcessFeed failed: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if mock.sent.Load() {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !mock.sent.Load() {
		t.Error("expected the notification to be delivered by the dispatcher")
	}
}
