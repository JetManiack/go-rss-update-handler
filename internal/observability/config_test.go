package observability_test

import (
	"testing"

	"github.com/JetManiack/go-rss-update-handler/internal/observability"
)

func TestConfig_Validate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		cfg     observability.Config
		wantErr bool
	}{
		{
			name: "valid info/json accepted",
			cfg: observability.Config{
				Log: observability.LogConfig{Level: "info", Format: "json"},
			},
			wantErr: false,
		},
		{
			name: "valid debug/text accepted",
			cfg: observability.Config{
				Log: observability.LogConfig{Level: "debug", Format: "text"},
			},
			wantErr: false,
		},
		{
			name: "valid warn/json accepted",
			cfg: observability.Config{
				Log: observability.LogConfig{Level: "warn", Format: "json"},
			},
			wantErr: false,
		},
		{
			name: "valid error/text accepted",
			cfg: observability.Config{
				Log: observability.LogConfig{Level: "error", Format: "text"},
			},
			wantErr: false,
		},
		{
			name: "bad level rejected",
			cfg: observability.Config{
				Log: observability.LogConfig{Level: "trace", Format: "json"},
			},
			wantErr: true,
		},
		{
			name: "empty level rejected",
			cfg: observability.Config{
				Log: observability.LogConfig{Level: "", Format: "json"},
			},
			wantErr: true,
		},
		{
			name: "bad format rejected",
			cfg: observability.Config{
				Log: observability.LogConfig{Level: "info", Format: "logfmt"},
			},
			wantErr: true,
		},
		{
			name: "empty format rejected",
			cfg: observability.Config{
				Log: observability.LogConfig{Level: "info", Format: ""},
			},
			wantErr: true,
		},
		{
			name: "both bad returns error",
			cfg: observability.Config{
				Log: observability.LogConfig{Level: "verbose", Format: "xml"},
			},
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
