package dispatcher

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/JetManiack/go-rss-update-handler/internal/model"
	"github.com/JetManiack/go-rss-update-handler/internal/storage"
)

func TestDispatcher(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	n := NewWebhookNotifier("hook", server.URL)
	svc := NewService([]Notifier{n})

	notif := Notification{
		Event: model.UpdateEvent{
			Title:       "Title",
			SourceURL:   "http://example.com",
			PublishedAt: time.Now(),
		},
		Verdict: storage.Verdict{
			Category: "release",
			Reason:   "important update",
		},
		FeedURL: "http://feed.com",
	}

	report, err := svc.Dispatch(context.Background(), notif, []string{"hook"})
	if err != nil {
		t.Fatalf("Dispatch failed: %v", err)
	}

	if err := report["hook"]; err != nil {
		t.Errorf("Webhook delivery failed: %v", err)
	}
}
