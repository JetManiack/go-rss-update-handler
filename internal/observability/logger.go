package observability

import (
	"io"
	"log/slog"
	"os"
	"strings"
)

func NewLogger(cfg LogConfig) *slog.Logger {
	return NewLoggerTo(os.Stdout, cfg)
}

func NewLoggerTo(w io.Writer, cfg LogConfig) *slog.Logger {
	var level slog.Level
	switch strings.ToLower(cfg.Level) {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}

	opts := &slog.HandlerOptions{Level: level}

	if cfg.Format == "json" {
		return slog.New(slog.NewJSONHandler(w, opts))
	}
	return slog.New(slog.NewTextHandler(w, opts))
}
