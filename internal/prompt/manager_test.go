package prompt

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestManager_ExecuteBuiltinClassify(t *testing.T) {
	m, err := New("")
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	data := map[string]any{
		"Current": map[string]any{"RawContent": "some content"},
		"History": []any{},
	}
	system, user, err := m.Execute(context.Background(), "classify", data)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if !strings.Contains(system, "JSON") {
		t.Errorf("system prompt should mention JSON:\n%s", system)
	}
	// User prompt must render the data and carry the exact schema, with no
	// leftover template syntax.
	for _, want := range []string{"some content", "## Current update", `"important"`, "No important-update history."} {
		if !strings.Contains(user, want) {
			t.Errorf("user prompt missing %q:\n%s", want, user)
		}
	}
	if strings.Contains(system, "{{") || strings.Contains(user, "{{") {
		t.Errorf("unrendered template syntax remains:\nsystem=%s\nuser=%s", system, user)
	}
}

func TestManager_UnknownPrompt(t *testing.T) {
	m, err := New("")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, _, err := m.Execute(context.Background(), "does-not-exist", nil); err == nil {
		t.Fatal("expected error for unknown prompt")
	}
}

// A user file overrides a built-in prompt by its `name` field, regardless of
// the file's own name.
func TestManager_OverrideByName(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "custom.yaml"), `name: classify
version: "9.9.9"
critical: true
system: |
  overridden system
user: |
  overridden user {{ .X }}
`)

	m, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	system, user, err := m.Execute(context.Background(), "classify", map[string]any{"X": "Z"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(system, "overridden system") {
		t.Errorf("built-in classify was not overridden by name; system=%q", system)
	}
	if !strings.Contains(user, "overridden user Z") {
		t.Errorf("override render wrong; user=%q", user)
	}
}

// When the `name` field is absent, identity falls back to the filename.
func TestManager_NameFallbackToFilename(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "greet.yaml"), `version: "1.0.0"
system: |
  s
user: |
  hi {{ .Who }}
`)

	m, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, user, err := m.Execute(context.Background(), "greet", map[string]any{"Who": "bob"})
	if err != nil {
		t.Fatalf("Execute greet: %v", err)
	}
	if !strings.Contains(user, "hi bob") {
		t.Errorf("filename fallback failed; user=%q", user)
	}
}

// A critical blueprint whose template does not compile is a fatal load error.
func TestManager_CriticalBrokenTemplate(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "broken.yaml"), `name: broken
version: "1.0.0"
critical: true
system: |
  ok
user: |
  {{ .Unclosed
`)
	if _, err := New(dir); err == nil {
		t.Fatal("expected New to fail on a critical broken template")
	}
}
