package storage

import (
	"context"
	"fmt"

	"gorm.io/gorm"
)

// RecentUpdates returns the most recently created updates (newest first) with
// their raw content preloaded. It backs the read-only web UI.
func RecentUpdates(ctx context.Context, db *gorm.DB, limit int) ([]Update, error) {
	if limit <= 0 {
		limit = 100
	}
	var updates []Update
	err := db.WithContext(ctx).
		Preload("RawContent").
		Order("created_at DESC").
		Limit(limit).
		Find(&updates).Error
	if err != nil {
		return nil, fmt.Errorf("storage: list recent updates: %w", err)
	}
	return updates, nil
}
