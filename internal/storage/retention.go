package storage

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"
)

// ParseRetention parses a raw-content retention period. It accepts standard Go
// durations (e.g. "720h", "30m") plus day ("d") and week ("w") suffixes
// (e.g. "90d", "2w"). An empty string or "0" means "keep forever" (returns 0).
func ParseRetention(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if s == "" || s == "0" {
		return 0, nil
	}
	if n, ok := strings.CutSuffix(s, "d"); ok {
		days, err := strconv.Atoi(n)
		if err != nil {
			return 0, fmt.Errorf("storage: invalid retention %q: %w", s, err)
		}
		return time.Duration(days) * 24 * time.Hour, nil
	}
	if n, ok := strings.CutSuffix(s, "w"); ok {
		weeks, err := strconv.Atoi(n)
		if err != nil {
			return 0, fmt.Errorf("storage: invalid retention %q: %w", s, err)
		}
		return time.Duration(weeks) * 7 * 24 * time.Hour, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("storage: invalid retention %q: %w", s, err)
	}
	return d, nil
}

// PurgeRawContent deletes raw_contents rows whose created_at is older than
// cutoff and returns the number of rows removed. updates rows (fingerprint and
// verdict) are never touched — they live forever.
func PurgeRawContent(ctx context.Context, db *gorm.DB, cutoff time.Time) (int64, error) {
	res := db.WithContext(ctx).Where("created_at < ?", cutoff).Delete(&RawContent{})
	if res.Error != nil {
		return 0, fmt.Errorf("storage: purge raw_contents: %w", res.Error)
	}
	return res.RowsAffected, nil
}

// StartRetentionJob periodically deletes raw_contents older than the retention
// window until ctx is cancelled. A non-positive retention disables the job
// (raw content is kept forever). It runs once immediately, then every interval.
func StartRetentionJob(ctx context.Context, db *gorm.DB, retention, interval time.Duration, log *slog.Logger) {
	if retention <= 0 {
		return
	}
	if interval <= 0 {
		interval = time.Hour
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		cutoff := time.Now().Add(-retention)
		removed, err := PurgeRawContent(ctx, db, cutoff)
		switch {
		case err != nil:
			log.Error("retention: purge raw_contents failed", "err", err)
		case removed > 0:
			log.Info("retention: purged raw_contents", "rows", removed, "older_than", cutoff)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
