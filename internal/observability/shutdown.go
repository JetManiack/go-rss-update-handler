package observability

import (
	"context"
	"os"
	"os/signal"
	"syscall"
)

func NotifyShutdown(ctx context.Context) (context.Context, context.CancelFunc) {
	return signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
}
