// Package llm holds LLM client configuration.
package llm

import (
	"errors"
	"fmt"
	"net/url"
)

// Config holds settings for the LLM HTTP client.
type Config struct {
	BaseURL string `koanf:"base_url"`
	Model   string `koanf:"model"`
	APIKey  string `koanf:"api_key"` // secret, populated from env GRUH_LLM_API_KEY
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

	return errors.Join(errs...)
}
