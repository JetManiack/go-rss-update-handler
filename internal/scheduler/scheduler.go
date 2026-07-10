package scheduler

import (
	"context"
	"math/rand/v2"
	"time"
)

// Task — задача, которую планировщик должен выполнять.
type Task func(ctx context.Context)

// Scheduler — простой планировщик.
type Scheduler struct {
	interval time.Duration
	jitter   float64 // от 0 до 1, доля интервала
}

func NewScheduler(interval time.Duration, jitter float64) *Scheduler {
	return &Scheduler{
		interval: interval,
		jitter:   jitter,
	}
}

// Start запускает выполнение задачи с заданным интервалом и джиттером.
func (s *Scheduler) Start(ctx context.Context, task Task) {
	for {
		task(ctx)
		
		delay := s.interval
		if s.jitter > 0 {
			// случайное смещение
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
