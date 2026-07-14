package orchestrator

import (
	"context"
	"errors"
	"log/slog"
	"sort"

	"github.com/JetManiack/go-rss-update-handler/internal/bus"
	"github.com/JetManiack/go-rss-update-handler/internal/classificator"
	"github.com/JetManiack/go-rss-update-handler/internal/collector"
	"github.com/JetManiack/go-rss-update-handler/internal/deduplicator"
	"github.com/JetManiack/go-rss-update-handler/internal/dispatcher"
	"github.com/JetManiack/go-rss-update-handler/internal/metrics"
	"github.com/JetManiack/go-rss-update-handler/internal/model"
	"github.com/JetManiack/go-rss-update-handler/internal/parser"
	"github.com/JetManiack/go-rss-update-handler/internal/storage"
	"github.com/google/uuid"
)

// reconcileBatch caps how many pending/undispatched updates a single
// ReconcilePending pass re-publishes; the rest are picked up on later passes.
const reconcileBatch = 500

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

	// Surface publish failures instead of silently dropping them: an inserted
	// update that was never published would otherwise be deduplicated forever
	// and never classified.
	return o.publishForClassification(ctx, inserted)
}

// messageFor builds the bus envelope for an update. RawContent may be nil (for
// example after retention cleanup), in which case the event carries empty
// content rather than panicking.
func messageFor(u storage.Update) bus.Message {
	var content string
	if u.RawContent != nil {
		content = u.RawContent.Content
	}
	return bus.Message{
		ID:      uuid.New().String(),
		Version: 1,
		Event: model.UpdateEvent{
			ID:          u.ID,
			FeedID:      u.FeedID,
			Title:       u.Title,
			SourceURL:   u.SourceURL,
			RawContent:  content,
			PublishedAt: u.PublishedAt,
			Fingerprint: u.Fingerprint,
		},
	}
}

// publishForClassification publishes updates to TopicUpdatesNew oldest-first, so
// each update is compared against genuinely earlier ones when the classificator
// builds its history. It joins any publish errors instead of dropping them.
func (o *Orchestrator) publishForClassification(ctx context.Context, updates []storage.Update) error {
	sort.SliceStable(updates, func(i, j int) bool {
		return updates[i].PublishedAt.Before(updates[j].PublishedAt)
	})
	var pubErrs []error
	for _, u := range updates {
		if err := o.bus.Publish(ctx, bus.TopicUpdatesNew, messageFor(u)); err != nil {
			o.logger.Error("failed to publish new update", "id", u.ID, "err", err)
			pubErrs = append(pubErrs, err)
		}
	}
	return errors.Join(pubErrs...)
}

// ReconcilePending re-drives work the in-memory bus cannot recover on its own:
// updates persisted but never classified (e.g. left in flight across a restart,
// or dropped after a classify error), and important updates that were never
// delivered. It re-publishes them to the classification and delivery topics,
// where the idempotent worker/dispatcher handle them exactly as new events.
// It is safe to call repeatedly: classified/delivered updates fall out of the
// queries, and re-processing overwrites verdicts / is dedup-guarded on delivery.
func (o *Orchestrator) ReconcilePending(ctx context.Context) error {
	var errs []error

	pending, err := o.updates.ListPending(ctx, reconcileBatch)
	if err != nil {
		errs = append(errs, err)
	} else if len(pending) > 0 {
		o.logger.Info("reconciling unclassified updates", "count", len(pending))
		if e := o.publishForClassification(ctx, pending); e != nil {
			errs = append(errs, e)
		}
	}

	undispatched, err := o.updates.ListUndispatchedImportant(ctx, reconcileBatch)
	if err != nil {
		errs = append(errs, err)
	} else if len(undispatched) > 0 {
		o.logger.Info("reconciling undispatched important updates", "count", len(undispatched))
		for _, u := range undispatched {
			if e := o.bus.Publish(ctx, bus.TopicUpdatesImportant, messageFor(u)); e != nil {
				o.logger.Error("failed to republish important update", "id", u.ID, "err", e)
				errs = append(errs, e)
			}
		}
	}

	return errors.Join(errs...)
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
