// Package observability holds observability/logging configuration.
package observability

import (
	"errors"
	"fmt"
)

// LogConfig holds structured-logging settings.
type LogConfig struct {
	Level  string `koanf:"level"`
	Format string `koanf:"format"`
}

// Config holds all observability settings.
type Config struct {
	Log LogConfig `koanf:"log"`
}

// Validate checks that Log.Level and Log.Format contain recognised values.
func (c Config) Validate() error {
	var errs []error

	switch c.Log.Level {
	case "debug", "info", "warn", "error":
		// valid
	default:
		errs = append(errs, fmt.Errorf(
			"observability: invalid log.level %q: must be one of debug|info|warn|error",
			c.Log.Level,
		))
	}

	switch c.Log.Format {
	case "text", "json":
		// valid
	default:
		errs = append(errs, fmt.Errorf(
			"observability: invalid log.format %q: must be one of text|json",
			c.Log.Format,
		))
	}

	return errors.Join(errs...)
}
