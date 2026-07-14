package orchestrator

import (
	"context"
	"testing"
	"time"

	"github.com/jetbrains/go-rss-update-handler/internal/dispatcher"
	"github.com/jetbrains/go-rss-update-handler/internal/storage"
)

type mockNotifier struct {
	sent bool
}

func (m *mockNotifier) Name() string { return "test" }
func (m *mockNotifier) Send(ctx context.Context, n dispatcher.Notification) error {
	m.sent = true
	return nil
}

func TestOrchestrator_RunWorker_DispatchesNotification(t *testing.T) {
	f := newTestFixture(t)
	mock := &mockNotifier{}
	f.orch.dispatcher = &dispatcher.Service{Notifiers: map[string]dispatcher.Notifier{"test": mock}} // Oops, need to fix access

	// Seed channel and mapping
	ch := storage.Channel{ID: "ch-1", Name: "test", Type: "webhook", ConfigJSON: "{}"}
	f.db.Create(&ch)
	f.db.Exec("INSERT INTO feed_channels (feed_id, channel_id) VALUES (?, ?)", f.feed.ID, ch.ID)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	go func() { _ = f.orch.RunWorker(ctx) }()
	time.Sleep(50 * time.Millisecond)

	if err := f.orch.ProcessFeed(ctx, f.feed); err != nil {
		t.Fatalf("ProcessFeed failed: %v", err)
	}

	time.Sleep(50 * time.Millisecond)
	if !mock.sent {
		t.Error("expected notification to be sent")
	}
}
