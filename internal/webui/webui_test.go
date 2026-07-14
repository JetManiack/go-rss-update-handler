package webui

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jetbrains/go-rss-update-handler/internal/storage"
)

func TestHandler_APIUpdates(t *testing.T) {
	imp := true
	fetch := func(_ context.Context, _ int) ([]storage.Update, error) {
		return []storage.Update{{
			ID:                "1",
			FeedID:            "f1",
			Title:             "v1.2.3",
			SourceURL:         "http://example.com/1",
			PublishedAt:       time.Now(),
			VerdictImportant:  &imp,
			VerdictCategory:   "security",
			VerdictConfidence: 0.9,
			VerdictReason:     "CVE fix",
			RawContent:        &storage.RawContent{Content: "release body"},
		}}, nil
	}

	rec := httptest.NewRecorder()
	Handler(fetch).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/updates", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}

	var got []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d rows, want 1", len(got))
	}
	if got[0]["content"] != "release body" {
		t.Errorf("content = %v", got[0]["content"])
	}
	if got[0]["title"] != "v1.2.3" {
		t.Errorf("title = %v", got[0]["title"])
	}
	if got[0]["category"] != "security" {
		t.Errorf("category = %v", got[0]["category"])
	}
	if got[0]["important"] != true {
		t.Errorf("important = %v", got[0]["important"])
	}
}

func TestHandler_ServesPage(t *testing.T) {
	rec := httptest.NewRecorder()
	Handler(func(context.Context, int) ([]storage.Update, error) { return nil, nil }).
		ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("content-type = %q", ct)
	}
	if !strings.Contains(rec.Body.String(), "/api/updates") {
		t.Error("page should reference the updates API")
	}
}
