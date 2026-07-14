// Package classificator turns updates into importance verdicts using the LLM.
package classificator

import "fmt"

// Config holds classificator tuning knobs.
type Config struct {
	// ConfidenceThreshold: an "important" verdict below this confidence is
	// treated as noise. Default 0.5.
	ConfidenceThreshold float64 `koanf:"confidence_threshold"`
	// MaxFormatRetries: how many extra times to re-ask the LLM when it returns
	// an unparseable/invalid response. Default 2.
	MaxFormatRetries int `koanf:"max_format_retries"`
}

// Validate checks the configuration bounds.
func (c Config) Validate() error {
	if c.ConfidenceThreshold < 0 || c.ConfidenceThreshold > 1 {
		return fmt.Errorf("classificator: confidence_threshold must be in [0,1]")
	}
	if c.MaxFormatRetries < 0 {
		return fmt.Errorf("classificator: max_format_retries must be >= 0")
	}
	return nil
}
