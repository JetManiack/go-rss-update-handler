package bus

import (
	"context"
	"testing"
	"time"
	"github.com/jetbrains/go-rss-update-handler/internal/model"
)

func TestMemoryBus(t *testing.T) {
	bus := NewMemoryBus()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	
	msg := Message{
		ID: "test-id",
		Event: model.UpdateEvent{
			SourceURL: "http://example.com",
		},
	}
	
	received := make(chan Message, 1)
	
	go func() {
		_ = bus.Subscribe(ctx, "updates.new", "", func(ctx context.Context, m Message) error {
			received <- m
			return nil
		})
	}()
	
	time.Sleep(10 * time.Millisecond)
	
	err := bus.Publish(ctx, "updates.new", msg)
	if err != nil {
		t.Errorf("Publish failed: %v", err)
	}
	
	select {
	case m := <-received:
		if m.ID != msg.ID {
			t.Errorf("Expected ID %s, got %s", msg.ID, m.ID)
		}
	case <-time.After(1 * time.Second):
		t.Error("Timed out waiting for message")
	}
}
