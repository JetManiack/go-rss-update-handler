package bus

import (
	"context"

	"github.com/jetbrains/go-rss-update-handler/internal/model"
)

// Topic names for the update pipeline. Use these constants instead of inline
// string literals so a typo cannot silently create a dead topic.
const (
	// TopicUpdatesNew carries freshly collected, deduplicated updates.
	TopicUpdatesNew = "updates.new"
	// TopicUpdatesImportant carries updates the classifier deemed important.
	TopicUpdatesImportant = "updates.important"
)

// Message is the event envelope on the bus.
type Message struct {
	ID       string            // uuid, for tracing
	Version  int               // envelope schema version
	Event    model.UpdateEvent // shared type from internal/model
	Metadata map[string]string // enrichment (feed_id, verdict, trace_id, ...)
}

// Handler is a message-handler function.
type Handler func(ctx context.Context, msg Message) error

// Bus is the event-bus interface.
type Bus interface {
	Publish(ctx context.Context, topic string, msg Message) error
	// Subscribe blocks until ctx is cancelled.
	Subscribe(ctx context.Context, topic, group string, handler Handler) error
}
