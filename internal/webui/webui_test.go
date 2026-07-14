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
	fetch := func(_ context.Context, _ Query) ([]storage.Update, int64, error) {
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
		}}, 1, nil
	}

	rec := httptest.NewRecorder()
	Handler(fetch).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/updates", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}

	var resp struct {
		Items []map[string]any `json:"items"`
		Total int              `json:"total"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Total != 1 {
		t.Errorf("total = %d, want 1", resp.Total)
	}
	if len(resp.Items) != 1 {
		t.Fatalf("items = %d, want 1", len(resp.Items))
	}
	it := resp.Items[0]
	if it["content"] != "release body" {
		t.Errorf("content = %v", it["content"])
	}
	if it["title"] != "v1.2.3" {
		t.Errorf("title = %v", it["title"])
	}
	if it["category"] != "security" {
		t.Errorf("category = %v", it["category"])
	}
	if it["important"] != true {
		t.Errorf("important = %v", it["important"])
	}
}

func TestHandler_ParsesQueryParams(t *testing.T) {
	var got Query
	fetch := func(_ context.Context, q Query) ([]storage.Update, int64, error) {
		got = q
		return nil, 0, nil
	}
	rec := httptest.NewRecorder()
	Handler(fetch).ServeHTTP(rec,
		httptest.NewRequest(http.MethodGet, "/api/updates?limit=10&offset=20&category=security&importance=important", nil))

	if got.Limit != 10 || got.Offset != 20 || got.Category != "security" || got.Importance != "important" {
		t.Errorf("parsed query = %+v", got)
	}
}

func TestHandler_SanitizesContent(t *testing.T) {
	fetch := func(_ context.Context, _ Query) ([]storage.Update, int64, error) {
		return []storage.Update{{
			ID:         "1",
			SourceURL:  "http://example.com/1",
			RawContent: &storage.RawContent{Content: `<script>alert(1)</script><p>hello <b>world</b></p>`},
		}}, 1, nil
	}
	rec := httptest.NewRecorder()
	Handler(fetch).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/updates", nil))

	var resp struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	content, _ := resp.Items[0]["content"].(string)
	if strings.Contains(strings.ToLower(content), "<script") {
		t.Errorf("script tag not sanitized: %q", content)
	}
	if !strings.Contains(content, "<p>hello") {
		t.Errorf("safe markup should be preserved: %q", content)
	}
}

func TestHandler_ServesPage(t *testing.T) {
	rec := httptest.NewRecorder()
	Handler(func(context.Context, Query) ([]storage.Update, int64, error) { return nil, 0, nil }).
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
