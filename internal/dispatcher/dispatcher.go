package dispatcher

import (
	"context"

	"github.com/JetManiack/go-rss-update-handler/internal/model"
	"github.com/JetManiack/go-rss-update-handler/internal/storage"
)

// Notification bundles the data needed to send a notification.
type Notification struct {
	Event   model.UpdateEvent
	Verdict storage.Verdict
	FeedURL string
}

// Notifier is the delivery transport interface.
type Notifier interface {
	Name() string
	Send(ctx context.Context, n Notification) error
}

// Report is the delivery result (channel name -> error).
type Report map[string]error

// Dispatcher is the notification-routing service.
type Dispatcher interface {
	Dispatch(ctx context.Context, n Notification, channels []string) (Report, error)
}
