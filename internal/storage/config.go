// Package storage holds storage-layer configuration.
package storage

import "fmt"

// Config holds database connection settings.
type Config struct {
	Driver              string `koanf:"driver"`
	DSN                 string `koanf:"dsn"` // secret, populated from env GRUH_DB_DSN
	MaxOpenConns        int    `koanf:"max_open_conns"`
	LogQueries          bool   `koanf:"log_queries"`
	RawContentRetention string `koanf:"raw_content_retention"`
}

// Validate checks that Driver is one of the supported values.
func (c Config) Validate() error {
	switch c.Driver {
	case "postgres", "sqlite":
		return nil
	default:
		return fmt.Errorf("storage: invalid driver %q: must be one of \"postgres\" or \"sqlite\"", c.Driver)
	}
}
