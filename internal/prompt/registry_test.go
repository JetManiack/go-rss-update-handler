package prompt

import (
	"os"
	"testing"
)

func TestRegistry_Render(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "prompt_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	reg, err := New(tmpDir)
	if err != nil {
		t.Fatal(err)
	}
	
	type Data struct {
		Current struct{ RawContent string }
		History []any
		Verdict struct{ Reason string }
	}
	
	data := Data{
		Current: struct{ RawContent string }{RawContent: "some content"},
		History: nil,
	}

	rendered, err := reg.Render("classify", data)
	if err != nil {
		t.Errorf("Render failed: %v", err)
	}
	if rendered == "" {
		t.Errorf("Rendered content is empty")
	}
}
