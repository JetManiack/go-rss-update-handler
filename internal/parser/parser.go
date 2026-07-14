package parser

import (
	"context"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jetbrains/go-rss-update-handler/internal/model"
	"github.com/mmcdole/gofeed"
)

// maxRawContentBytes caps the RawContent stored per entry, bounding memory and
// DB usage on pathological feeds.
const maxRawContentBytes = 64 * 1024

type Parser struct {
	feedParser *gofeed.Parser
}

func NewParser() *Parser {
	return &Parser{
		feedParser: gofeed.NewParser(),
	}
}

func (p *Parser) Parse(_ context.Context, feedURL string, body []byte) ([]model.UpdateEvent, error) {
	feed, err := p.feedParser.Parse(strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}

	events := make([]model.UpdateEvent, 0, len(feed.Items))
	for _, item := range feed.Items {
		pub := item.PublishedParsed
		if pub == nil {
			if item.UpdatedParsed != nil {
				pub = item.UpdatedParsed
			} else {
				// Use current time as fallback if no date is present.
				now := time.Now()
				pub = &now
			}
		}

		content := item.Content
		if content == "" {
			content = item.Description
		}
		if content == "" {
			content = item.Title
		}
		content = truncate(content, maxRawContentBytes)

		// SourceURL is the entry link, falling back to the feed URL so that
		// entries without a <link> are not dropped.
		sourceURL := item.Link
		if sourceURL == "" {
			sourceURL = feedURL
		}

		events = append(events, model.UpdateEvent{
			Title:       item.Title,
			SourceURL:   sourceURL,
			RawContent:  content,
			PublishedAt: pub.UTC(),
		})
	}

	// Return in publication order, newest first.
	sort.SliceStable(events, func(i, j int) bool {
		return events[i].PublishedAt.After(events[j].PublishedAt)
	})
	return events, nil
}

// truncate limits s to at most maxBytes bytes without splitting a UTF-8 rune.
func truncate(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	b := s[:maxBytes]
	for len(b) > 0 && !utf8.ValidString(b) {
		b = b[:len(b)-1]
	}
	return b
}
