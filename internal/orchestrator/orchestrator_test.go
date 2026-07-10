package orchestrator

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"github.com/jetbrains/go-rss-update-handler/internal/bus"
	"github.com/jetbrains/go-rss-update-handler/internal/collector"
	"github.com/jetbrains/go-rss-update-handler/internal/deduplicator"
	"github.com/jetbrains/go-rss-update-handler/internal/parser"
	"github.com/jetbrains/go-rss-update-handler/internal/storage"
	"time"
)

type mockFeedRepo struct{ storage.FeedRepo }
func (r *mockFeedRepo) UpdateCacheHeaders(ctx context.Context, id, etag, lm string) error { return nil }

type mockUpdateRepo struct{ storage.UpdateRepo }
func (r *mockUpdateRepo) InsertNew(ctx context.Context, u []storage.Update) ([]storage.Update, error) { return u, nil }

func TestOrchestrator(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0">
  <channel>
    <item>
      <title>Item 1</title>
      <link>http://example.com/1</link>
      <pubDate>Wed, 21 Oct 2025 07:28:00 GMT</pubDate>
    </item>
  </channel>
</rss>`))
	}))
	defer server.Close()

	b := bus.NewMemoryBus()
	c := collector.NewCollector(collector.Config{
		Timeout:     time.Second,
		RatePerHost: 10,
		Retries:     1,
		BackoffBase: time.Millisecond,
		UserAgent:   "test",
	})
	p := parser.NewParser()
	d := deduplicator.NewDeduplicator()
	o := NewOrchestrator(c, p, d, b, &mockFeedRepo{}, &mockUpdateRepo{}, nil, slog.Default())
	
	received := make(chan bus.Message, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	
	go func() {
		_ = b.Subscribe(ctx, "updates.new", "", func(ctx context.Context, msg bus.Message) error {
			received <- msg
			return nil
		})
	}()
	time.Sleep(10 * time.Millisecond)
	
	err := o.ProcessFeed(context.Background(), storage.Feed{ID: "feed-1", URL: server.URL})
	if err != nil {
		t.Fatalf("ProcessFeed failed: %v", err)
	}
	
	select {
	case msg := <-received:
		if msg.Event.Fingerprint == "" {
			// This was expected before, but let's see what's actually happening
			// It seems Deduplicator.Fingerprint wasn't called or produced empty
			// Actually I need to check the event fingerprint
			t.Logf("Fingerprint: %s", msg.Event.Fingerprint)
			t.Errorf("Expected fingerprint, got empty")
		}
	case <-time.After(1 * time.Second):
		t.Error("Timed out waiting for message")
	}
}
