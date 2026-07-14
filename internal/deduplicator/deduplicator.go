package deduplicator

import (
	"crypto/sha256"
	"encoding/hex"
	"time"

	"github.com/jetbrains/go-rss-update-handler/internal/model"
)

// Deduplicator computes a content-independent fingerprint for an event.
type Deduplicator struct{}

func NewDeduplicator() *Deduplicator {
	return &Deduplicator{}
}

// Fingerprint computes a stable fingerprint for an UpdateEvent from its
// SourceURL and publication time. The raw content is deliberately excluded so
// that edits to the entry body do not produce a "new" event — which would
// otherwise trigger duplicate notifications for an already-seen update.
func (d *Deduplicator) Fingerprint(e *model.UpdateEvent) string {
	h := sha256.New()
	h.Write([]byte(e.SourceURL))
	h.Write([]byte("\n"))
	h.Write([]byte(e.PublishedAt.UTC().Format(time.RFC3339)))
	return hex.EncodeToString(h.Sum(nil))
}
