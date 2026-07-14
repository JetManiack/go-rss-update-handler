package orchestrator

import (
	"context"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jetbrains/go-rss-update-handler/internal/bus"
	"github.com/jetbrains/go-rss-update-handler/internal/classificator"
	"github.com/jetbrains/go-rss-update-handler/internal/collector"
	"github.com/jetbrains/go-rss-update-handler/internal/deduplicator"
	"github.com/jetbrains/go-rss-update-handler/internal/dispatcher"
	"github.com/jetbrains/go-rss-update-handler/internal/model"
	"github.com/jetbrains/go-rss-update-handler/internal/parser"
	"github.com/jetbrains/go-rss-update-handler/internal/storage"
)

type Orchestrator struct {
	collector    *collector.Collector
	parser       *parser.Parser
	deduplicator *deduplicator.Deduplicator
	bus          bus.Bus
	feeds        storage.FeedRepo
	updates      storage.UpdateRepo
	classifier   classificator.Service
	dispatcher   dispatcher.Dispatcher
	logger       *slog.Logger
}

func NewOrchestrator(
	c *collector.Collector,
	p *parser.Parser,
	d *deduplicator.Deduplicator,
	b bus.Bus,
	feeds storage.FeedRepo,
	updates storage.UpdateRepo,
	classifier classificator.Service,
	dispatcher dispatcher.Dispatcher,
	logger *slog.Logger,
) *Orchestrator {
	return &Orchestrator{c, p, d, b, feeds, updates, classifier, dispatcher, logger}
}

func (o *Orchestrator) ProcessFeed(ctx context.Context, feed storage.Feed) error {
	res, err := o.collector.Fetch(ctx, collector.FeedRef{
		FeedID:       feed.ID,
		URL:          feed.URL,
		ETag:         feed.Etag,
		LastModified: feed.LastModified,
	})
	if err != nil {
		return err
	}
	if res.NotModified {
		return nil
	}

	if err := o.feeds.UpdateCacheHeaders(ctx, feed.ID, res.ETag, res.LastModified); err != nil {
		o.logger.Error("failed to update cache headers", "err", err)
	}

	events, err := o.parser.Parse(ctx, feed.URL, res.Body)
	if err != nil {
		return err
	}

	var updates []storage.Update
	for _, e := range events {
		e.Fingerprint = o.deduplicator.Fingerprint(&e)
		updates = append(updates, storage.Update{
			FeedID:      feed.ID,
			Fingerprint: e.Fingerprint,
			SourceURL:   e.SourceURL,
			PublishedAt: e.PublishedAt,
			RawContent:  &storage.RawContent{Content: e.RawContent},
		})
	}

	inserted, err := o.updates.InsertNew(ctx, updates)
	if err != nil {
		return err
	}

	for _, u := range inserted {
		msg := bus.Message{
			ID:      uuid.New().String(),
			Version: 1,
			Event: model.UpdateEvent{
				ID:          u.ID,
				FeedID:      u.FeedID,
				SourceURL:   u.SourceURL,
				RawContent:  u.RawContent.Content,
				PublishedAt: u.PublishedAt,
				Fingerprint: u.Fingerprint,
			},
		}
		if err := o.bus.Publish(ctx, "updates.new", msg); err != nil {
			o.logger.Error("failed to publish update", "err", err)
		}
	}

	return nil
}

func (o *Orchestrator) RunWorker(ctx context.Context) error {
	return o.bus.Subscribe(ctx, "updates.new", "classificator", func(ctx context.Context, msg bus.Message) error {
		verdict, err := o.classifier.Classify(ctx, msg.Event)
		if err != nil {
			return err
		}
		if err := o.updates.SaveVerdict(ctx, msg.Event.ID, storage.Verdict{
			Important:  verdict.Important,
			Category:   verdict.Category,
			Confidence: verdict.Confidence,
			Reason:     verdict.Reason,
		}); err != nil {
			return err
		}
		if verdict.Important {
			if err := o.bus.Publish(ctx, "updates.classified", msg); err != nil {
				return err
			}
			channels, err := o.feeds.ChannelsFor(ctx, msg.Event.FeedID)
			if err != nil {
				return err
			}
			if len(channels) == 0 {
				return nil
			}
			channelIDs := make([]string, len(channels))
			for i, name := range channels {
				channelIDs[i] = name
			}
			_, err = o.dispatcher.Dispatch(ctx, dispatcher.Notification{
				Event:   msg.Event,
				Verdict: verdict,
				FeedURL: msg.Event.SourceURL,
			}, channelIDs)
			return err
		}
		return nil
	})
}
