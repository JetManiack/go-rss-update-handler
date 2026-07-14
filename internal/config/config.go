// Package config loads, merges, and validates application-wide configuration.
package config

import (
	"errors"
	"fmt"
	"os"

	"time"

	"github.com/go-viper/mapstructure/v2"
	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/env"
	"github.com/knadh/koanf/providers/file"
	koanf "github.com/knadh/koanf/v2"

	"github.com/jetbrains/go-rss-update-handler/internal/classificator"
	"github.com/jetbrains/go-rss-update-handler/internal/collector"
	"github.com/jetbrains/go-rss-update-handler/internal/llm"
	"github.com/jetbrains/go-rss-update-handler/internal/observability"
	"github.com/jetbrains/go-rss-update-handler/internal/storage"
)

type SchedulerConfig struct {
	Interval time.Duration `koanf:"interval"`
	Jitter   time.Duration `koanf:"jitter"`
}

type PromptConfig struct {
	Dir string `koanf:"dir"`
}

type DispatcherConfig struct {
	Slack    map[string]string            `koanf:"slack"`
	Telegram map[string]map[string]string `koanf:"telegram"`
}

// Config is the root application configuration.
type Config struct {
	Storage       storage.Config       `koanf:"storage"`
	LLM           llm.Config           `koanf:"llm"`
	Observability observability.Config `koanf:"observability"`
	Scheduler     SchedulerConfig      `koanf:"scheduler"`
	Collector     collector.Config     `koanf:"collector"`
	Prompt        PromptConfig         `koanf:"prompt"`
	Dispatcher    DispatcherConfig     `koanf:"dispatcher"`
	Classificator classificator.Config `koanf:"classificator"`
	Feeds         []string             `koanf:"feeds"`
}

// envKeyMap is the closed set of environment variable → koanf key path mappings.
var envKeyMap = map[string]string{
	"GRUH_STORAGE_DRIVER":     "storage.driver",
	"GRUH_DB_DSN":             "storage.dsn",
	"GRUH_LLM_BASE_URL":       "llm.base_url",
	"GRUH_LLM_MODEL":          "llm.model",
	"GRUH_LLM_API_KEY":        "llm.api_key",
	"GRUH_LLM_TIMEOUT":        "llm.timeout",
	"GRUH_LLM_RETRIES":        "llm.max_retries",
	"GRUH_LLM_CONCURRENT":     "llm.max_concurrent",
	"GRUH_LLM_TEMP":           "llm.temperature",
	"GRUH_SCHEDULER_INTERVAL": "scheduler.interval",
	"GRUH_SCHEDULER_JITTER":   "scheduler.jitter",
	"GRUH_COLLECTOR_TIMEOUT":  "collector.timeout",
	"GRUH_COLLECTOR_RETRIES":  "collector.retries",
	"GRUH_LOG_LEVEL":          "observability.log.level",
	"GRUH_LOG_FORMAT":         "observability.log.format",
}

// Load reads configuration from (in order of increasing precedence):
//  1. Hard-coded defaults.
//  2. The YAML file at path (skipped silently if the file does not exist).
//  3. Environment variables with the GRUH_ prefix.
//
// It then unmarshals the result into a *Config with strict unknown-key checking
// and calls Validate().
func Load(path string) (*Config, error) {
	k := koanf.New(".")

	// 1. Defaults.
	defaults := map[string]any{
		"observability.log.level":            "info",
		"observability.log.format":           "json",
		"llm.timeout":                        "60s",
		"llm.max_retries":                    3,
		"llm.max_concurrent":                 4,
		"llm.temperature":                    0.1,
		"scheduler.interval":                 "1h",
		"scheduler.jitter":                   "5m",
		"collector.timeout":                  "30s",
		"collector.rate_per_host":            1.0,
		"collector.retries":                  3,
		"collector.backoff_base":             "1s",
		"collector.user_agent":               "gruh/1.0",
		"prompt.dir":                         "", // empty = built-in prompts only
		"classificator.confidence_threshold": 0.5,
		"classificator.max_format_retries":   2,
	}
	for key, val := range defaults {
		if err := k.Set(key, val); err != nil {
			return nil, fmt.Errorf("config: set default %q: %w", key, err)
		}
	}

	// 2. Optional YAML file.
	if _, statErr := os.Stat(path); statErr == nil {
		// File exists — load it.
		fp := file.Provider(path)
		if err := k.Load(fp, yaml.Parser()); err != nil {
			return nil, fmt.Errorf("config: load file %q: %w", path, err)
		}
	} else if !os.IsNotExist(statErr) {
		// os.Stat failed for a reason other than "not found".
		return nil, fmt.Errorf("config: stat file %q: %w", path, statErr)
	}

	// 3. Environment variables.
	ep := env.Provider("GRUH_", ".", func(s string) string {
		mapped, ok := envKeyMap[s]
		if !ok {
			return "" // ignore unknown env vars
		}
		return mapped
	})
	if err := k.Load(ep, nil); err != nil {
		return nil, fmt.Errorf("config: load env: %w", err)
	}

	// 4. Unmarshal with strict unknown-key detection.
	var cfg Config
	err := k.UnmarshalWithConf("", &cfg, koanf.UnmarshalConf{
		Tag: "koanf",
		DecoderConfig: &mapstructure.DecoderConfig{
			DecodeHook: mapstructure.ComposeDecodeHookFunc(
				mapstructure.StringToTimeDurationHookFunc(),
			),
			Result:           &cfg,
			WeaklyTypedInput: true,
			ErrorUnused:      true,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("config: unmarshal: %w", err)
	}

	// 5. Validate.
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return &cfg, nil
}

func (c *DispatcherConfig) Validate() error {
	return nil
}

// Validate runs validation on every configuration section and returns all
// errors joined together so that the caller sees every violation at once.
func (c *Config) Validate() error {
	var errs []error

	if err := c.Storage.Validate(); err != nil {
		errs = append(errs, fmt.Errorf("storage: %w", err))
	}
	if err := c.LLM.Validate(); err != nil {
		errs = append(errs, fmt.Errorf("llm: %w", err))
	}
	if err := c.Observability.Validate(); err != nil {
		errs = append(errs, fmt.Errorf("observability: %w", err))
	}
	if err := c.Collector.Validate(); err != nil {
		errs = append(errs, fmt.Errorf("collector: %w", err))
	}
	if err := c.Dispatcher.Validate(); err != nil {
		errs = append(errs, fmt.Errorf("dispatcher: %w", err))
	}
	if err := c.Classificator.Validate(); err != nil {
		errs = append(errs, fmt.Errorf("classificator: %w", err))
	}

	return errors.Join(errs...)
}
