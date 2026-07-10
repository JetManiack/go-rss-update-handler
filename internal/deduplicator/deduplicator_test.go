package deduplicator

import (
	"testing"
	"github.com/jetbrains/go-rss-update-handler/internal/model"
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
