package scheduler

import (
	"context"
	"math/rand/v2"
	"time"
)

// Task is the job the scheduler must run.
type Task func(ctx context.Context)

type Locker interface {
	Lock(ctx context.Context, key string, ttl time.Duration) (func(), error)
}

// Scheduler is a simple scheduler.
type Scheduler struct {
	interval time.Duration
	jitter   float64
	locker   Locker
}

func NewScheduler(interval time.Duration, jitter float64, locker Locker) *Scheduler {
	return &Scheduler{
		interval: interval,
		jitter:   jitter,
		locker:   locker,
	}
}

// Start runs the task at the configured interval with jitter.
func (s *Scheduler) Start(ctx context.Context, key string, task Task) {
	for {
		if s.locker != nil {
			unlock, err := s.locker.Lock(ctx, key, s.interval)
			if err != nil {
				// failed to acquire lock, skip this iteration
			} else {
				task(ctx)
				unlock()
			}
		} else {
			task(ctx)
		}

		delay := s.interval
		if s.jitter > 0 {
			// random offset
			// #nosec G404
			jitter := time.Duration((rand.Float64()*2 - 1) * s.jitter * float64(s.interval))
			delay += jitter
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
	}
}
