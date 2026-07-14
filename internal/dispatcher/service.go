package dispatcher

import (
	"context"
	"fmt"
	"sync"

	"golang.org/x/sync/errgroup"
)

type Service struct {
	Notifiers map[string]Notifier
}

func NewService(notifiers []Notifier) *Service {
	m := make(map[string]Notifier)
	for _, n := range notifiers {
		m[n.Name()] = n
	}
	return &Service{Notifiers: m}
}

func (s *Service) Dispatch(ctx context.Context, n Notification, channelIDs []string) (Report, error) {
	report := make(Report)
	var mu sync.Mutex

	g, gCtx := errgroup.WithContext(ctx)

	for _, id := range channelIDs {
		id := id
		n_arg, ok := s.Notifiers[id]
		if !ok {
			mu.Lock()
			report[id] = fmt.Errorf("notifier not found: %s", id)
			mu.Unlock()
			continue
		}

		g.Go(func() error {
			err := n_arg.Send(gCtx, n)
			mu.Lock()
			report[id] = err
			mu.Unlock()
			return nil
		})
	}

	_ = g.Wait()
	return report, nil
}
