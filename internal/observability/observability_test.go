package observability

import (
	"bytes"
	"context"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestLogger(t *testing.T) {
	buf := new(bytes.Buffer)
	cfg := LogConfig{Level: "debug", Format: "json"}
	logger := NewLoggerTo(buf, cfg)

	logger.Debug("test")
	if !strings.Contains(buf.String(), `"level":"DEBUG"`) {
		t.Errorf("expected debug level in json, got: %s", buf.String())
	}
}

func TestShutdown(t *testing.T) {
	ctx, cancel := NotifyShutdown(context.Background())
	defer cancel()

	// Simulate signal
	go func() {
		time.Sleep(10 * time.Millisecond)
		_ = syscall.Kill(syscall.Getpid(), syscall.SIGTERM)
	}()

	select {
	case <-ctx.Done():
		// ok
	case <-time.After(100 * time.Millisecond):
		t.Error("timeout waiting for shutdown signal")
	}
}
