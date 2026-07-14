// Package llm holds LLM client configuration.
package llm

import (
	"errors"
	"fmt"
	"net/url"
	"time"
)

// TLSConfig holds TLS options for the LLM HTTP client.
type TLSConfig struct {
	// Insecure disables server certificate verification. Use only for
	// self-signed or local endpoints (e.g. a local vLLM over https).
	Insecure bool `koanf:"insecure"`
}

// Config holds settings for the LLM HTTP client.
type Config struct {
	BaseURL       string        `koanf:"base_url"`
	Model         string        `koanf:"model"`
	APIKey        string        `koanf:"api_key"` // secret, populated from env GRUH_LLM_API_KEY
	Timeout       time.Duration `koanf:"timeout"`
	MaxRetries    int           `koanf:"max_retries"`
	MaxConcurrent int           `koanf:"max_concurrent"`
	Temperature   float64       `koanf:"temperature"`
	TLS           TLSConfig     `koanf:"tls"`
}

// Validate checks that BaseURL is a valid absolute http/https URL and that
// Model is non-empty.
func (c Config) Validate() error {
	var errs []error

	if c.BaseURL == "" {
		errs = append(errs, fmt.Errorf("llm: base_url must not be empty"))
	} else {
		u, err := url.Parse(c.BaseURL)
		if err != nil || !u.IsAbs() || (u.Scheme != "http" && u.Scheme != "https") {
			errs = append(errs, fmt.Errorf("llm: base_url %q must be an absolute URL with scheme http or https", c.BaseURL))
		}
	}

	if c.Model == "" {
		errs = append(errs, fmt.Errorf("llm: model must not be empty"))
	}

	if c.Timeout <= 0 {
		errs = append(errs, fmt.Errorf("llm: timeout must be positive"))
	}
	if c.MaxRetries < 0 {
		errs = append(errs, fmt.Errorf("llm: max_retries must be non-negative"))
	}
	if c.MaxConcurrent <= 0 {
		errs = append(errs, fmt.Errorf("llm: max_concurrent must be positive"))
	}
	if c.Temperature < 0 || c.Temperature > 2 {
		errs = append(errs, fmt.Errorf("llm: temperature must be between 0 and 2"))
	}

	return errors.Join(errs...)
}
