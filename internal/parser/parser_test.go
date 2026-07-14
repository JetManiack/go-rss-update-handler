package parser

import (
	"context"
	"os"
	"strings"
	"testing"
)

func TestParser_Parse(t *testing.T) {
	body, err := os.ReadFile("testdata/feed.xml")
	if err != nil {
		t.Fatalf("Failed to read testdata: %v", err)
	}

	p := NewParser()
	events, err := p.Parse(context.Background(), "http://example.com/feed", body)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if len(events) != 1 {
		t.Errorf("Expected 1 event, got %d", len(events))
	}

	if events[0].SourceURL != "http://example.com/1" {
		t.Errorf("Expected http://example.com/1, got %s", events[0].SourceURL)
	}
}

const rssFallbackAndOrder = `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0"><channel>
  <title>t</title>
  <item><title>Older</title><link>http://example.com/old</link><pubDate>Mon, 01 Jan 2024 00:00:00 GMT</pubDate></item>
  <item><title>Newer no link</title><description>body</description><pubDate>Wed, 01 Jan 2025 00:00:00 GMT</pubDate></item>
</channel></rss>`

// A link-less entry must fall back to the feed URL, and events must be newest-first.
func TestParser_FeedURLFallbackAndOrdering(t *testing.T) {
	p := NewParser()
	events, err := p.Parse(context.Background(), "http://feed.example/atom", []byte(rssFallbackAndOrder))
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}
	if !events[0].PublishedAt.After(events[1].PublishedAt) {
		t.Errorf("events must be newest-first: %v then %v", events[0].PublishedAt, events[1].PublishedAt)
	}
	if events[0].SourceURL != "http://feed.example/atom" {
		t.Errorf("link-less entry should fall back to feed URL, got %q", events[0].SourceURL)
	}
	if events[1].SourceURL != "http://example.com/old" {
		t.Errorf("entry with a link should keep it, got %q", events[1].SourceURL)
	}
}

// Oversized content must be truncated to the byte cap.
func TestParser_TruncatesLongContent(t *testing.T) {
	long := strings.Repeat("a", 100*1024)
	rss := `<?xml version="1.0" encoding="UTF-8"?><rss version="2.0"><channel><title>t</title>` +
		`<item><title>x</title><link>http://e/1</link><description>` + long + `</description></item></channel></rss>`
	p := NewParser()
	events, err := p.Parse(context.Background(), "http://feed", []byte(rss))
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if len(events[0].RawContent) > maxRawContentBytes {
		t.Errorf("content not truncated: %d bytes (cap %d)", len(events[0].RawContent), maxRawContentBytes)
	}
}
