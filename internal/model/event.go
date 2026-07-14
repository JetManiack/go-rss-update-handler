package model

import "time"

// UpdateEvent is the core of the system's data model.
type UpdateEvent struct {
	ID          string // filled in after persistent storage (storage.Update.ID)
	FeedID      string
	Title       string
	SourceURL   string    // entry URL (link) or feed URL
	RawContent  string    // normalized entry content
	PublishedAt time.Time // UTC
	Fingerprint string    // filled in by the deduplicator
}
