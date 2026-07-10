package parser

import (
	"context"
	"strings"
	"time"

	"github.com/jetbrains/go-rss-update-handler/internal/model"
	"github.com/mmcdole/gofeed"
)

type Parser struct {
	feedParser *gofeed.Parser
}

func NewParser() *Parser {
	return &Parser{
		feedParser: gofeed.NewParser(),
	}
}

func (p *Parser) Parse(ctx context.Context, feedURL string, body []byte) ([]model.UpdateEvent, error) {
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
				// Use current time as fallback if no date is present
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

		events = append(events, model.UpdateEvent{
			SourceURL:   item.Link,
			RawContent:  content,
			PublishedAt: pub.UTC(),
		})
	}
	return events, nil
}
