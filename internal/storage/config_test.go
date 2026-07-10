package storage_test

import (
	"testing"

	"go-rss-update-handler/internal/storage"
)

func TestConfig_Validate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		driver  string
		wantErr bool
	}{
		{name: "postgres accepted", driver: "postgres", wantErr: false},
		{name: "sqlite accepted", driver: "sqlite", wantErr: false},
		{name: "empty driver rejected", driver: "", wantErr: true},
		{name: "mysql rejected", driver: "mysql", wantErr: true},
		{name: "unknown value rejected", driver: "mssql", wantErr: true},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c := storage.Config{Driver: tc.driver, DSN: "any"}
			err := c.Validate()
			if tc.wantErr && err == nil {
				t.Errorf("Validate() = nil, want error for driver=%q", tc.driver)
			}
			if !tc.wantErr && err != nil {
				t.Errorf("Validate() = %v, want nil for driver=%q", err, tc.driver)
			}
		})
	}
}
