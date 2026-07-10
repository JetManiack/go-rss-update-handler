package orchestrator

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"github.com/jetbrains/go-rss-update-handler/internal/bus"
	"github.com/jetbrains/go-rss-update-handler/internal/collector"
	"github.com/jetbrains/go-rss-update-handler/internal/deduplicator"
	"github.com/jetbrains/go-rss-update-handler/internal/parser"
	"time"
)

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
	o := NewOrchestrator(c, p, d, b)
	
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
	
	err := o.ProcessFeed(context.Background(), server.URL)
	if err != nil {
		t.Fatalf("ProcessFeed failed: %v", err)
	}
	
	select {
	case msg := <-received:
		if msg.Event.Fingerprint == "" {
			t.Errorf("Expected fingerprint, got empty")
		}
	case <-time.After(1 * time.Second):
		t.Error("Timed out waiting for message")
	}
}
