package deduplicator

import (
	"crypto/sha256"
	"encoding/hex"
	"github.com/jetbrains/go-rss-update-handler/internal/model"
)

// Deduplicator вычисляет отпечаток (fingerprint) для события.
type Deduplicator struct{}

func NewDeduplicator() *Deduplicator {
	return &Deduplicator{}
}

// Fingerprint вычисляет уникальный отпечаток для UpdateEvent.
func (d *Deduplicator) Fingerprint(e *model.UpdateEvent) string {
	// Fingerprint основывается на SourceURL и содержимом контента
	h := sha256.New()
	h.Write([]byte(e.SourceURL))
	h.Write([]byte(e.RawContent))
	return hex.EncodeToString(h.Sum(nil))
}
