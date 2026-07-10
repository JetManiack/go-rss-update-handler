package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"go-rss-update-handler/internal/config"
)

// validEnv sets all required secret env vars so that Validate() passes in tests
// that also need secrets.
func validEnv(t *testing.T) {
	t.Helper()
	t.Setenv("GRUH_DB_DSN", "postgres://localhost/test")
	t.Setenv("GRUH_LLM_API_KEY", "sk-test")
}

// writeTempYAML creates a temp file with the given content and returns its path.
func writeTempYAML(t *testing.T, content string) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "config-*.yaml")
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	if _, err := f.WriteString(content); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close temp file: %v", err)
	}
	return f.Name()
}

// TestLoad_MergesFileAndValidates loads a full valid YAML and checks that the
// parsed struct has the expected field values and Validate() returns nil.
func TestLoad_MergesFileAndValidates(t *testing.T) {
	validEnv(t)

	yaml := `
storage:
  driver: postgres
llm:
  base_url: https://vllm.internal:8000/v1
  model: qwen3-32b
observability:
  log:
    level: info
    format: json
`
	path := writeTempYAML(t, yaml)
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}

	if cfg.Storage.Driver != "postgres" {
		t.Errorf("Storage.Driver = %q, want %q", cfg.Storage.Driver, "postgres")
	}
	if cfg.LLM.BaseURL != "https://vllm.internal:8000/v1" {
		t.Errorf("LLM.BaseURL = %q, want %q", cfg.LLM.BaseURL, "https://vllm.internal:8000/v1")
	}
	if cfg.LLM.Model != "qwen3-32b" {
		t.Errorf("LLM.Model = %q, want %q", cfg.LLM.Model, "qwen3-32b")
	}
	if cfg.Observability.Log.Level != "info" {
		t.Errorf("Observability.Log.Level = %q, want %q", cfg.Observability.Log.Level, "info")
	}
	if cfg.Observability.Log.Format != "json" {
		t.Errorf("Observability.Log.Format = %q, want %q", cfg.Observability.Log.Format, "json")
	}
}

// TestLoad_EnvOverridesFile verifies env vars take precedence over the YAML file.
func TestLoad_EnvOverridesFile(t *testing.T) {
	validEnv(t)
	t.Setenv("GRUH_LLM_MODEL", "llama3-overridden")

	yaml := `
storage:
  driver: sqlite
llm:
  base_url: https://example.com
  model: original-model
observability:
  log:
    level: debug
    format: text
`
	path := writeTempYAML(t, yaml)
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.LLM.Model != "llama3-overridden" {
		t.Errorf("LLM.Model = %q, want %q (env override)", cfg.LLM.Model, "llama3-overridden")
	}
}

// TestLoad_MissingFileIsNotError verifies that a non-existent file path is OK.
func TestLoad_MissingFileIsNotError(t *testing.T) {
	validEnv(t)
	// Provide all required fields via env vars.
	t.Setenv("GRUH_STORAGE_DRIVER", "postgres")
	t.Setenv("GRUH_LLM_BASE_URL", "https://vllm.internal:8000/v1")
	t.Setenv("GRUH_LLM_MODEL", "qwen3-32b")
	t.Setenv("GRUH_LOG_LEVEL", "info")
	t.Setenv("GRUH_LOG_FORMAT", "json")

	nonExistent := filepath.Join(t.TempDir(), "does-not-exist.yaml")
	cfg, err := config.Load(nonExistent)
	if err != nil {
		t.Fatalf("Load() with missing file: unexpected error = %v", err)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

// TestLoad_UnknownKeyInYAMLErrors verifies that unknown YAML keys cause an error.
func TestLoad_UnknownKeyInYAMLErrors(t *testing.T) {
	validEnv(t)

	yaml := `
storage:
  driver: postgres
  bogus: 1
llm:
  base_url: https://example.com
  model: some-model
observability:
  log:
    level: info
    format: json
`
	path := writeTempYAML(t, yaml)
	_, err := config.Load(path)
	if err == nil {
		t.Fatal("Load() = nil, want error for unknown YAML key")
	}
}

// TestLoad_DefaultsApplied verifies that when a YAML file omits the
// observability section, the defaults (info/json) are applied.
func TestLoad_DefaultsApplied(t *testing.T) {
	validEnv(t)

	// YAML with no observability section at all.
	yaml := `
storage:
  driver: postgres
llm:
  base_url: https://example.com
  model: some-model
`
	path := writeTempYAML(t, yaml)
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Observability.Log.Level != "info" {
		t.Errorf("default log.level = %q, want %q", cfg.Observability.Log.Level, "info")
	}
	if cfg.Observability.Log.Format != "json" {
		t.Errorf("default log.format = %q, want %q", cfg.Observability.Log.Format, "json")
	}
}

// TestLoad_GoldenConfigExampleYAML is the golden test: Load the repo-root
// config.example.yaml, provide secrets via env, and assert no error.
func TestLoad_GoldenConfigExampleYAML(t *testing.T) {
	validEnv(t)

	// The test lives in internal/config/, so ../../config.example.yaml
	// resolves to the repository root.
	path := "../../config.example.yaml"
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load(config.example.yaml) error = %v", err)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

// TestValidate_ReturnsAllErrors ensures that Validate collects errors from all
// sections and returns them together (not just the first one).
func TestValidate_ReturnsAllErrors(t *testing.T) {
	// Deliberately invalid in every section.
	cfg := &config.Config{}
	// Storage.Driver = "" — invalid
	// LLM.BaseURL = "", LLM.Model = "" — both invalid
	// Observability.Log.Level = "", Observability.Log.Format = "" — both invalid

	err := cfg.Validate()
	if err == nil {
		t.Fatal("Validate() = nil, want errors from all sections")
	}
}

// TestLoad_EnvOnlyStorageDriver verifies GRUH_STORAGE_DRIVER env var mapping.
func TestLoad_EnvVarMappings(t *testing.T) {
	validEnv(t)
	t.Setenv("GRUH_STORAGE_DRIVER", "sqlite")
	t.Setenv("GRUH_LLM_BASE_URL", "https://vllm.test")
	t.Setenv("GRUH_LLM_MODEL", "test-model")
	t.Setenv("GRUH_LOG_LEVEL", "warn")
	t.Setenv("GRUH_LOG_FORMAT", "text")

	// Use a YAML that only partially fills in — env should complete the rest.
	yaml := `
storage:
  driver: postgres
llm:
  base_url: https://original.com
  model: original
observability:
  log:
    level: debug
    format: json
`
	path := writeTempYAML(t, yaml)
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	// env should override everything
	if cfg.Storage.Driver != "sqlite" {
		t.Errorf("Storage.Driver = %q, want sqlite", cfg.Storage.Driver)
	}
	if cfg.LLM.BaseURL != "https://vllm.test" {
		t.Errorf("LLM.BaseURL = %q, want https://vllm.test", cfg.LLM.BaseURL)
	}
	if cfg.LLM.Model != "test-model" {
		t.Errorf("LLM.Model = %q, want test-model", cfg.LLM.Model)
	}
	if cfg.LLM.APIKey != "sk-test" {
		t.Errorf("LLM.APIKey = %q, want sk-test", cfg.LLM.APIKey)
	}
	if cfg.Storage.DSN != "postgres://localhost/test" {
		t.Errorf("Storage.DSN = %q, want postgres://localhost/test", cfg.Storage.DSN)
	}
	if cfg.Observability.Log.Level != "warn" {
		t.Errorf("Log.Level = %q, want warn", cfg.Observability.Log.Level)
	}
	if cfg.Observability.Log.Format != "text" {
		t.Errorf("Log.Format = %q, want text", cfg.Observability.Log.Format)
	}
}
