package scheduler

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestScheduler(t *testing.T) {
	s := NewScheduler(50*time.Millisecond, 0, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	var count atomic.Int32

	s.Start(ctx, "test-lock", func(ctx context.Context) {
		count.Add(1)
	})

	// Expect about 4 calls (200ms / 50ms)
	if count.Load() < 3 {
		t.Errorf("Expected at least 3 calls, got %d", count.Load())
	}
}
