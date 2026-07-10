package bus

import (
	"context"
	"github.com/jetbrains/go-rss-update-handler/internal/model"
)

// Message представляет собой конверт события в шине.
type Message struct {
	ID       string            // uuid, для трассировки
	Version  int               // версия схемы конверта
	Event    model.UpdateEvent // общий тип из internal/model
	Metadata map[string]string // обогащение (feed_id, verdict, trace_id, ...)
}

// Handler — функция-обработчик сообщения.
type Handler func(ctx context.Context, msg Message) error

// Bus — интерфейс шины событий.
type Bus interface {
	Publish(ctx context.Context, topic string, msg Message) error
	// Subscribe блокируется до отмены ctx.
	Subscribe(ctx context.Context, topic, group string, handler Handler) error
}
