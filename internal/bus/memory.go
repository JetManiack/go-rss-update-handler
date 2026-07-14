package bus

import (
	"context"
	"log/slog"
	"sync"
)

// defaultBufferSize is the per-subscription channel buffer for the in-memory bus.
const defaultBufferSize = 1024

// subscription is a single consumer's buffered mailbox.
type subscription struct {
	handler Handler
	ch      chan Message
}

// MemoryBus is an in-memory, channel-based Bus implementation. Publish enqueues
// a message onto each subscriber's buffered channel (it does not run handlers
// inline), so producers are decoupled from consumers. Handler errors are logged
// (there is no redelivery in memory mode — that is the Redis bus's job).
type MemoryBus struct {
	mu      sync.RWMutex
	subs    map[string][]*subscription
	bufSize int
	logger  *slog.Logger
}

func NewMemoryBus() *MemoryBus {
	return &MemoryBus{
		subs:    make(map[string][]*subscription),
		bufSize: defaultBufferSize,
		logger:  slog.Default(),
	}
}

// Publish enqueues msg onto every subscriber of topic. It blocks only if a
// subscriber's buffer is full, and returns early if ctx is cancelled.
func (b *MemoryBus) Publish(ctx context.Context, topic string, msg Message) error {
	b.mu.RLock()
	subs := b.subs[topic]
	b.mu.RUnlock()

	for _, s := range subs {
		select {
		case s.ch <- msg:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

// Subscribe registers a consumer for topic and processes messages until ctx is
// cancelled. Each Subscribe call is an independent consumer (fan-out).
func (b *MemoryBus) Subscribe(ctx context.Context, topic, group string, handler Handler) error {
	s := &subscription{handler: handler, ch: make(chan Message, b.bufSize)}

	b.mu.Lock()
	b.subs[topic] = append(b.subs[topic], s)
	b.mu.Unlock()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case msg := <-s.ch:
			if err := handler(ctx, msg); err != nil {
				b.logger.Error("bus: handler error", "topic", topic, "group", group, "err", err)
			}
		}
	}
}
