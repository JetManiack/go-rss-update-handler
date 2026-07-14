package deduplicator

import (
	"crypto/sha256"
	"encoding/hex"
	"github.com/jetbrains/go-rss-update-handler/internal/model"
)

// Deduplicator computes a fingerprint for an event.
type Deduplicator struct{}

func NewDeduplicator() *Deduplicator {
	return &Deduplicator{}
}

// Fingerprint computes a unique fingerprint for an UpdateEvent.
func (d *Deduplicator) Fingerprint(e *model.UpdateEvent) string {
	// Fingerprint is based on SourceURL and the content body
	h := sha256.New()
	h.Write([]byte(e.SourceURL))
	h.Write([]byte(e.RawContent))
	return hex.EncodeToString(h.Sum(nil))
}
