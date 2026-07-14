package deduplicator

import (
	"testing"
	"time"

	"github.com/JetManiack/go-rss-update-handler/internal/model"
)

func TestDeduplicator_Fingerprint(t *testing.T) {
	d := NewDeduplicator()

	e1 := &model.UpdateEvent{SourceURL: "http://url1", RawContent: "content1"}
	e2 := &model.UpdateEvent{SourceURL: "http://url1", RawContent: "content1"}
	e3 := &model.UpdateEvent{SourceURL: "http://url2", RawContent: "content1"}

	f1 := d.Fingerprint(e1)
	f2 := d.Fingerprint(e2)
	f3 := d.Fingerprint(e3)

	if f1 != f2 {
		t.Errorf("Fingerprints should be equal for identical events")
	}
	if f1 == f3 {
		t.Errorf("Fingerprints should be different for different events")
	}
}

// Editing the body of an already-seen entry must NOT change its fingerprint,
// otherwise the update would be re-notified as new.
func TestDeduplicator_Fingerprint_ContentIndependent(t *testing.T) {
	d := NewDeduplicator()
	at := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

	a := &model.UpdateEvent{SourceURL: "https://x/1", PublishedAt: at, RawContent: "v1"}
	b := &model.UpdateEvent{SourceURL: "https://x/1", PublishedAt: at, RawContent: "v2 edited"}
	if d.Fingerprint(a) != d.Fingerprint(b) {
		t.Error("fingerprint must not depend on RawContent")
	}

	// A different publication time is a different event.
	c := &model.UpdateEvent{SourceURL: "https://x/1", PublishedAt: at.Add(time.Hour)}
	if d.Fingerprint(a) == d.Fingerprint(c) {
		t.Error("different PublishedAt must yield a different fingerprint")
	}
}
