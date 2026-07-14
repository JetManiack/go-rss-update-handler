package storage

import (
	"context"
	"fmt"

	"gorm.io/gorm"
)

// ListUpdates returns a filtered, paginated page of updates ordered by
// publication time (newest first), plus the total number of matching rows, with
// raw content preloaded. It backs the read-only web UI.
//
// category "" matches any category. importance is one of "" (any), "important",
// "noise", or "pending" (not yet classified).
func ListUpdates(ctx context.Context, db *gorm.DB, category, importance string, limit, offset int) ([]Update, int64, error) {
	if limit <= 0 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}

	q := db.WithContext(ctx).Model(&Update{})
	if category != "" {
		q = q.Where("verdict_category = ?", category)
	}
	switch importance {
	case "important":
		q = q.Where("verdict_important = ?", true)
	case "noise":
		q = q.Where("verdict_important = ?", false)
	case "pending":
		q = q.Where("classified_at IS NULL")
	}

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("storage: count updates: %w", err)
	}

	var items []Update
	if err := q.Preload("RawContent").
		Order("published_at DESC, created_at DESC").
		Limit(limit).Offset(offset).
		Find(&items).Error; err != nil {
		return nil, 0, fmt.Errorf("storage: list updates: %w", err)
	}
	return items, total, nil
}
