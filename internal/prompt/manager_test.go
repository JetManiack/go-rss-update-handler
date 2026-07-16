package prompt

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
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

// A non-existent override directory must be ignored (built-ins only), not fatal.
func TestManager_MissingOverrideDir(t *testing.T) {
	m, err := New(filepath.Join(t.TempDir(), "does-not-exist"))
	if err != nil {
		t.Fatalf("New with a missing override dir must not fail: %v", err)
	}
	if _, _, err := m.Execute(context.Background(), "classify", map[string]any{
		"Current": map[string]any{"RawContent": "x"},
		"History": []any{},
	}); err != nil {
		t.Fatalf("built-in classify must still work: %v", err)
	}
}

// The built-in classify blueprint must ship a structured-output schema whose
// contract matches the classificator's verdict fields.
func TestManager_BuiltinClassifyHasSchema(t *testing.T) {
	m, err := New("")
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	raw, name, ok := m.Schema("classify")
	if !ok {
		t.Fatal("built-in classify must declare a schema")
	}
	if name != "classify_verdict" {
		t.Errorf("schema name = %q, want %q", name, "classify_verdict")
	}

	var s struct {
		Type                 string         `json:"type"`
		AdditionalProperties bool           `json:"additionalProperties"`
		Properties           map[string]any `json:"properties"`
		Required             []string       `json:"required"`
	}
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatalf("classify schema is not valid JSON: %v\nraw=%s", err, raw)
	}
	if s.Type != "object" {
		t.Errorf(`schema type = %q, want "object"`, s.Type)
	}
	if s.AdditionalProperties {
		t.Error("schema must set additionalProperties:false for strict mode")
	}
	// strict mode requires every property to be present in required.
	want := []string{"title", "important", "category", "release_level", "confidence", "reason"}
	for _, field := range want {
		if _, has := s.Properties[field]; !has {
			t.Errorf("schema missing property %q", field)
		}
		if !slices.Contains(s.Required, field) {
			t.Errorf("schema field %q not in required", field)
		}
	}
	if len(s.Properties) != len(want) {
		t.Errorf("schema has %d properties, want %d: %v", len(s.Properties), len(want), s.Properties)
	}
}

// A blueprint that declares a schema block exposes it via Schema(): the raw
// JSON schema and its name, with ok == true.
func TestManager_Schema(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "sample.yaml"), `name: sample
version: "1.0.0"
critical: true
system: |
  s
user: |
  u
schema:
  name: sample_out
  schema:
    type: object
    additionalProperties: false
    properties:
      foo:
        type: string
    required:
      - foo
`)

	m, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	raw, name, ok := m.Schema("sample")
	if !ok {
		t.Fatal("expected ok=true for a blueprint with a schema")
	}
	if name != "sample_out" {
		t.Errorf("schema name = %q, want %q", name, "sample_out")
	}

	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("schema is not valid JSON: %v\nraw=%s", err, raw)
	}
	if got["type"] != "object" {
		t.Errorf(`schema["type"] = %v, want "object"`, got["type"])
	}
	if got["additionalProperties"] != false {
		t.Errorf(`schema["additionalProperties"] = %v, want false`, got["additionalProperties"])
	}
	props, _ := got["properties"].(map[string]any)
	if _, hasFoo := props["foo"]; !hasFoo {
		t.Errorf("schema missing properties.foo; got=%v", got)
	}
}

// A blueprint without a schema block returns ok == false.
func TestManager_SchemaAbsent(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "plain.yaml"), `name: plain
version: "1.0.0"
critical: true
system: |
  s
user: |
  u
`)

	m, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	raw, name, ok := m.Schema("plain")
	if ok {
		t.Errorf("expected ok=false for a schema-less blueprint; got name=%q raw=%s", name, raw)
	}
	if raw != nil {
		t.Errorf("expected nil raw schema; got %s", raw)
	}
}

// When schema.name is omitted, it defaults to the blueprint name.
func TestManager_SchemaNameDefaultsToBlueprintName(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "noname.yaml"), `name: noname
version: "1.0.0"
critical: true
system: |
  s
user: |
  u
schema:
  schema:
    type: object
    additionalProperties: false
    properties:
      a:
        type: string
    required:
      - a
`)

	m, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, name, ok := m.Schema("noname")
	if !ok {
		t.Fatal("expected ok=true")
	}
	if name != "noname" {
		t.Errorf("schema name = %q, want blueprint name %q", name, "noname")
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
