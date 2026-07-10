package parser

import (
	"context"
	"os"
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
