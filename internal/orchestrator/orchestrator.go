package orchestrator

import (
	"context"
	"github.com/google/uuid"
	"github.com/jetbrains/go-rss-update-handler/internal/bus"
	"github.com/jetbrains/go-rss-update-handler/internal/collector"
	"github.com/jetbrains/go-rss-update-handler/internal/deduplicator"
	"github.com/jetbrains/go-rss-update-handler/internal/parser"
)

type Orchestrator struct {
	collector    *collector.Collector
	parser       *parser.Parser
	deduplicator *deduplicator.Deduplicator
	bus          bus.Bus
}

func NewOrchestrator(c *collector.Collector, p *parser.Parser, d *deduplicator.Deduplicator, b bus.Bus) *Orchestrator {
	return &Orchestrator{c, p, d, b}
}

func (o *Orchestrator) ProcessFeed(ctx context.Context, feedURL string) error {
	// В реальной жизни нужно хранить состояние ETag/LastModified в БД, 
	// здесь для упрощения — пусто.
	body, _, _, err := o.collector.Fetch(ctx, feedURL, "", "")
	if err != nil {
		return err
	}
	if body == nil {
		return nil // 304
	}

	events, err := o.parser.Parse(ctx, feedURL, body)
	if err != nil {
		return err
	}

	for _, e := range events {
		e.Fingerprint = o.deduplicator.Fingerprint(&e)
		
		msg := bus.Message{
			ID:      uuid.New().String(),
			Version: 1,
			Event:   e,
		}
		
		if err := o.bus.Publish(ctx, "updates.new", msg); err != nil {
			return err
		}
	}
	
	return nil
}
