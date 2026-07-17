package webui

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/JetManiack/go-rss-update-handler/internal/storage"
)

// Minimal Atom shapes for asserting the feed structure in tests.
type tAtomLink struct {
	Href string `xml:"href,attr"`
	Rel  string `xml:"rel,attr"`
	Type string `xml:"type,attr"`
}
type tAtomCategory struct {
	Term   string `xml:"term,attr"`
	Scheme string `xml:"scheme,attr"`
}
type tAtomEntry struct {
	ID         string          `xml:"id"`
	Title      string          `xml:"title"`
	Updated    string          `xml:"updated"`
	Links      []tAtomLink     `xml:"link"`
	Categories []tAtomCategory `xml:"category"`
	Summary    string          `xml:"summary"`
	Content    string          `xml:"content"`
}
type tAtomFeed struct {
	XMLName xml.Name     `xml:"feed"`
	Title   string       `xml:"title"`
	ID      string       `xml:"id"`
	Links   []tAtomLink  `xml:"link"`
	Updated string       `xml:"updated"`
	Entries []tAtomEntry `xml:"entry"`
}

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

func TestHandler_FeedAtom(t *testing.T) {
	imp := true
	pub := time.Date(2026, 7, 16, 10, 0, 0, 0, time.UTC)
	fetch := func(_ context.Context, _ Query) ([]storage.Update, int64, error) {
		return []storage.Update{{
			ID:                "abc-123",
			FeedID:            "f1",
			Title:             "proj v1.2.3",
			SourceURL:         "http://example.com/releases/tag/v1.2.3",
			PublishedAt:       pub,
			VerdictImportant:  &imp,
			VerdictCategory:   "security",
			VerdictConfidence: 0.9,
			VerdictReason:     "CVE fix",
			RawContent:        &storage.RawContent{Content: `<script>alert(1)</script><p>hello <b>world</b></p>`},
		}}, 1, nil
	}

	rec := httptest.NewRecorder()
	Handler(fetch).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/feed.atom", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/atom+xml") {
		t.Errorf("content-type = %q, want application/atom+xml", ct)
	}

	var feed tAtomFeed
	if err := xml.Unmarshal(rec.Body.Bytes(), &feed); err != nil {
		t.Fatalf("feed is not well-formed XML: %v\n%s", err, rec.Body.String())
	}
	if len(feed.Entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(feed.Entries))
	}
	e := feed.Entries[0]
	if e.Title != "proj v1.2.3" {
		t.Errorf("entry title = %q", e.Title)
	}
	if e.ID != "urn:uuid:abc-123" {
		t.Errorf("entry id = %q, want urn:uuid:abc-123", e.ID)
	}
	// alternate link points at the release page.
	var haveAlt bool
	for _, l := range e.Links {
		if l.Href == "http://example.com/releases/tag/v1.2.3" {
			haveAlt = true
		}
	}
	if !haveAlt {
		t.Errorf("entry missing alternate link to SourceURL: %+v", e.Links)
	}
	// two categories: the classifier category and the importance term.
	var haveCat, haveImp bool
	for _, c := range e.Categories {
		if c.Term == "security" {
			haveCat = true
		}
		if c.Term == "important" && c.Scheme == "urn:gruh:importance" {
			haveImp = true
		}
	}
	if !haveCat {
		t.Errorf("missing category term=security: %+v", e.Categories)
	}
	if !haveImp {
		t.Errorf("missing importance category term=important: %+v", e.Categories)
	}
	// verdict in the summary.
	if !strings.Contains(e.Summary, "CVE fix") {
		t.Errorf("summary should carry the verdict reason: %q", e.Summary)
	}
	// content sanitized: script gone, safe markup kept.
	if strings.Contains(strings.ToLower(e.Content), "<script") {
		t.Errorf("script not sanitized in content: %q", e.Content)
	}
	if !strings.Contains(e.Content, "hello") {
		t.Errorf("safe content dropped: %q", e.Content)
	}
}

func TestHandler_FeedAtomThreadsFiltersAndCap(t *testing.T) {
	var got Query
	fetch := func(_ context.Context, q Query) ([]storage.Update, int64, error) {
		got = q
		return nil, 0, nil
	}
	rec := httptest.NewRecorder()
	Handler(fetch).ServeHTTP(rec,
		httptest.NewRequest(http.MethodGet, "/feed.atom?category=security&importance=important", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	if got.Category != "security" || got.Importance != "important" {
		t.Errorf("filters not threaded: %+v", got)
	}
	if got.Limit != feedMaxItems {
		t.Errorf("limit = %d, want feedMaxItems=%d", got.Limit, feedMaxItems)
	}
	if got.Offset != 0 {
		t.Errorf("offset = %d, want 0", got.Offset)
	}
}

func TestHandler_FeedAtomEmpty(t *testing.T) {
	fetch := func(_ context.Context, _ Query) ([]storage.Update, int64, error) {
		return nil, 0, nil
	}
	rec := httptest.NewRecorder()
	Handler(fetch).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/feed.atom", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	var feed tAtomFeed
	if err := xml.Unmarshal(rec.Body.Bytes(), &feed); err != nil {
		t.Fatalf("empty feed must still be well-formed XML: %v\n%s", err, rec.Body.String())
	}
	if len(feed.Entries) != 0 {
		t.Errorf("expected 0 entries, got %d", len(feed.Entries))
	}
}

func TestHandler_PageHasFeedAutodiscovery(t *testing.T) {
	rec := httptest.NewRecorder()
	Handler(func(context.Context, Query) ([]storage.Update, int64, error) { return nil, 0, nil }).
		ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	body := rec.Body.String()
	if !strings.Contains(body, `type="application/atom+xml"`) || !strings.Contains(body, `href="/feed.atom"`) {
		t.Errorf("page missing Atom auto-discovery <link>:\n%s", body)
	}
}

func TestHandler_ServesIcon(t *testing.T) {
	rec := httptest.NewRecorder()
	Handler(func(context.Context, Query) ([]storage.Update, int64, error) { return nil, 0, nil }).
		ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/go-ruh.png", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "image/png" {
		t.Errorf("content-type = %q, want image/png", ct)
	}
	if rec.Body.Len() == 0 {
		t.Error("icon body is empty")
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
