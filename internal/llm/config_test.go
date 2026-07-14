package llm_test

import (
	"testing"
	"time"

	"github.com/JetManiack/go-rss-update-handler/internal/llm"
)

func TestConfig_Validate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		cfg     llm.Config
		wantErr bool
	}{
		{
			name: "valid config accepted",
			cfg: llm.Config{
				BaseURL:       "https://vllm.internal:8000/v1",
				Model:         "qwen3-32b",
				APIKey:        "sk-secret",
				Timeout:       time.Second,
				MaxRetries:    3,
				MaxConcurrent: 4,
				Temperature:   0.1,
			},
			wantErr: false,
		},
		{
			name:    "empty BaseURL rejected",
			cfg:     llm.Config{BaseURL: "", Model: "some-model", Timeout: time.Second, MaxConcurrent: 1},
			wantErr: true,
		},
		{
			name:    "non-URL BaseURL rejected",
			cfg:     llm.Config{BaseURL: "not a url at all", Model: "some-model", Timeout: time.Second, MaxConcurrent: 1},
			wantErr: true,
		},
		{
			name:    "relative URL rejected",
			cfg:     llm.Config{BaseURL: "/relative/path", Model: "some-model", Timeout: time.Second, MaxConcurrent: 1},
			wantErr: true,
		},
		{
			name:    "ftp scheme rejected",
			cfg:     llm.Config{BaseURL: "ftp://example.com", Model: "some-model", Timeout: time.Second, MaxConcurrent: 1},
			wantErr: true,
		},
		{
			name:    "http scheme accepted",
			cfg:     llm.Config{BaseURL: "http://localhost:11434", Model: "some-model", Timeout: time.Second, MaxConcurrent: 1},
			wantErr: false,
		},
		{
			name:    "empty Model rejected",
			cfg:     llm.Config{BaseURL: "https://example.com", Model: "", Timeout: time.Second, MaxConcurrent: 1},
			wantErr: true,
		},
		{
			name:    "both empty returns error",
			cfg:     llm.Config{BaseURL: "", Model: "", Timeout: time.Second, MaxConcurrent: 1},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := tc.cfg.Validate()
			if tc.wantErr && err == nil {
				t.Errorf("Validate() = nil, want error for config %+v", tc.cfg)
			}
			if !tc.wantErr && err != nil {
				t.Errorf("Validate() = %v, want nil for config %+v", err, tc.cfg)
			}
		})
	}
}
