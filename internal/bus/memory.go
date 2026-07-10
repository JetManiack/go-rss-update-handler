package bus

import (
	"context"
	"fmt"
	"sync"
)

// MemoryBus — in-memory реализация шины на каналах.
type MemoryBus struct {
	mu          sync.RWMutex
	subscribers map[string][]Handler
}

func NewMemoryBus() *MemoryBus {
	return &MemoryBus{
		subscribers: make(map[string][]Handler),
	}
}

func (b *MemoryBus) Publish(ctx context.Context, topic string, msg Message) error {
	b.mu.RLock()
	handlers, ok := b.subscribers[topic]
	b.mu.RUnlock()
	if !ok {
		return nil
	}

	for _, h := range handlers {
		if err := h(ctx, msg); err != nil {
			return fmt.Errorf("handler error: %w", err)
		}
	}
	return nil
}

func (b *MemoryBus) Subscribe(ctx context.Context, topic, group string, handler Handler) error {
	b.mu.Lock()
	b.subscribers[topic] = append(b.subscribers[topic], handler)
	b.mu.Unlock()

	<-ctx.Done()
	return ctx.Err()
}
