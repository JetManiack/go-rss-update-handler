package orchestrator

import (
	"context"
	"errors"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jetbrains/go-rss-update-handler/internal/bus"
	"github.com/jetbrains/go-rss-update-handler/internal/classificator"
	"github.com/jetbrains/go-rss-update-handler/internal/collector"
	"github.com/jetbrains/go-rss-update-handler/internal/deduplicator"
	"github.com/jetbrains/go-rss-update-handler/internal/dispatcher"
	"github.com/jetbrains/go-rss-update-handler/internal/metrics"
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
		metrics.FeedFetches.WithLabelValues("error").Inc()
		return err
	}
	if res.NotModified {
		metrics.FeedFetches.WithLabelValues("not_modified").Inc()
		return nil
	}
	metrics.FeedFetches.WithLabelValues("ok").Inc()

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
			Title:       e.Title,
			SourceURL:   e.SourceURL,
			PublishedAt: e.PublishedAt,
			RawContent:  &storage.RawContent{Content: e.RawContent},
		})
	}

	inserted, err := o.updates.InsertNew(ctx, updates)
	if err != nil {
		return err
	}
	metrics.UpdatesNew.Add(float64(len(inserted)))
	o.logger.Info("feed processed", "feed", feed.URL, "parsed", len(events), "new", len(inserted))

	var pubErrs []error
	for _, u := range inserted {
		msg := bus.Message{
			ID:      uuid.New().String(),
			Version: 1,
			Event: model.UpdateEvent{
				ID:          u.ID,
				FeedID:      u.FeedID,
				Title:       u.Title,
				SourceURL:   u.SourceURL,
				RawContent:  u.RawContent.Content,
				PublishedAt: u.PublishedAt,
				Fingerprint: u.Fingerprint,
			},
		}
		if err := o.bus.Publish(ctx, bus.TopicUpdatesNew, msg); err != nil {
			o.logger.Error("failed to publish new update", "id", u.ID, "err", err)
			pubErrs = append(pubErrs, err)
		}
	}

	// Surface publish failures instead of silently dropping them: an inserted
	// update that was never published would otherwise be deduplicated forever
	// and never classified.
	return errors.Join(pubErrs...)
}

// RunWorker consumes new updates, classifies them, persists the verdict, and
// publishes important ones for delivery. It does NOT dispatch itself — delivery
// (and its idempotency) is the dispatcher role's job (RunDispatcher).
func (o *Orchestrator) RunWorker(ctx context.Context) error {
	return o.bus.Subscribe(ctx, bus.TopicUpdatesNew, "classificator", func(ctx context.Context, msg bus.Message) error {
		// The orchestrator supplies the classification context (recent important
		// updates for the feed); the classificator does not touch storage.
		history, err := o.updates.LastImportant(ctx, msg.Event.FeedID, 2)
		if err != nil {
			return err
		}
		o.logger.Debug("classifying update", "id", msg.Event.ID, "url", msg.Event.SourceURL)
		verdict, err := o.classifier.Classify(ctx, msg.Event, history)
		if err != nil {
			return err
		}
		if err := o.updates.SaveVerdict(ctx, msg.Event.ID, verdict); err != nil {
			return err
		}
		o.logger.Info("update classified",
			"url", msg.Event.SourceURL,
			"important", verdict.Important,
			"category", verdict.Category,
			"confidence", verdict.Confidence,
			"reason", verdict.Reason)
		if verdict.Important {
			metrics.Classifications.WithLabelValues("important").Inc()
			return o.bus.Publish(ctx, bus.TopicUpdatesImportant, msg)
		}
		metrics.Classifications.WithLabelValues("noise").Inc()
		return nil
	})
}

// RunDispatcher consumes important updates and delivers them to each mapped
// channel exactly once, guarding against duplicate sends via the dispatches
// table (IsDispatched / MarkDispatched).
func (o *Orchestrator) RunDispatcher(ctx context.Context) error {
	return o.bus.Subscribe(ctx, bus.TopicUpdatesImportant, "dispatcher", func(ctx context.Context, msg bus.Message) error {
		verdict, err := o.updates.GetVerdict(ctx, msg.Event.ID)
		if err != nil {
			return err
		}
		channels, err := o.feeds.ChannelsFor(ctx, msg.Event.FeedID)
		if err != nil {
			return err
		}

		var pending []string
		for _, ch := range channels {
			dispatched, err := o.updates.IsDispatched(ctx, msg.Event.ID, ch)
			if err != nil {
				return err
			}
			if !dispatched {
				pending = append(pending, ch)
			}
		}
		if len(pending) == 0 {
			return nil
		}

		if _, err := o.dispatcher.Dispatch(ctx, dispatcher.Notification{
			Event:   msg.Event,
			Verdict: verdict,
			FeedURL: msg.Event.SourceURL,
		}, pending); err != nil {
			metrics.Dispatches.WithLabelValues("error").Inc()
			return err
		}
		metrics.Dispatches.WithLabelValues("ok").Add(float64(len(pending)))
		for _, ch := range pending {
			if err := o.updates.MarkDispatched(ctx, msg.Event.ID, ch); err != nil {
				return err
			}
		}
		o.logger.Info("update dispatched", "url", msg.Event.SourceURL, "channels", len(pending))
		return nil
	})
}
