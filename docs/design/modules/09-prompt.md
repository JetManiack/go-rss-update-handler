# 09. Prompt (`internal/prompt`)

## 1. Purpose

Prompt management for the LLM. Each prompt is a single `.yaml` **blueprint** that holds both a
`system` and a `user` Go template (`text/template`). Built-in blueprints are shipped in the binary
via `go:embed`; the user can override any of them with files in their own directory — **a user file
takes priority over the built-in one, matched by the `name` field**.

## 2. Responsibilities and Boundaries

**Does:**
* Loads blueprints once at startup: embedded first, then a user directory layered on top.
* Renders the `system` and `user` templates with typed data (`text/template`).
* Validates templates at load time (all built-in + overridden ones must compile).

**Does NOT:**
* Does not call the LLM and does not know the semantics of prompts.
* Does not edit prompts itself — only reads and renders.
* Does not reload prompts at runtime (no hot-reload — changes are applied by restarting; see §9).

## 3. Public Interface

```go
// Manager loads and renders prompt blueprints (concrete type).
func New(overrideDir string) (*Manager, error) // overrideDir may be empty

// Execute renders the named blueprint with data and returns (system, user).
func (m *Manager) Execute(ctx context.Context, name string, data any) (system, user string, err error)
```

Consumers depend on their own minimal interface (accept-interfaces / return-structs). For example,
`internal/classificator` declares:

```go
type PromptManager interface {
	Execute(ctx context.Context, name string, data any) (system, user string, err error)
}
```

## 4. Internal Design

### File Structure

```
internal/prompt/
├── manager.go
└── builtin/            # embedded via go:embed
    └── classify.yaml   # classification blueprint (system + user)
```

* `//go:embed builtin` → `embed.FS`; `fs.WalkDir` loads every `*.yaml` / `*.yml`.
* **Identity by `name`:** a blueprint is keyed by its `name` field. If `name` is empty, it falls back
  to the filename (without extension). The filename is otherwise irrelevant.
* **Override by `name`:** embedded blueprints are loaded first; then each YAML file under `overrideDir`
  is loaded and replaces any blueprint with the same `name`, regardless of the file's own name.
* All templates are parsed once at `Manager` creation; errors are handled by the `critical` gate below.
* Data for `classify`: current update, slice of important-update history, feed metadata.

### Blueprint Format and Versioning

Each blueprint is a YAML document:

```yaml
name: classify
version: "2.0.0"
critical: true
description: "Classify a project update against the feed's recent important updates"

system: |
  You are an expert at analyzing software-project updates.
  Your task is to classify each update as important (requires attention) or noise.
  Always respond strictly as JSON, with no extra text.
user: |
  Assess the importance of the project update.

  ## Current update
  {{ .Current.RawContent }}

  ## Most recent important updates (for comparison)
  {{ range .History }}- {{ .PublishedAt }}: {{ .VerdictReason }}
  {{ else }}No important-update history.
  {{ end }}
  Respond strictly as JSON: {"important": bool, "category": string, "confidence": float, "reason": string}
```

* `name` — prompt identity; used as the map key and for override matching (see above).
* `version` — semantic version (semver); written into LLM traces (`prompt_version`,
  see [12-observability.md](12-observability.md)) and used in eval (phase 7). When overriding a
  built-in blueprint, the user should set their own version so traces show which variant produced the
  verdict.
* `critical` — the blueprint is required for pipeline operation: a broken/uncompilable template is a
  fatal error at startup; for non-critical ones — a warning and the blueprint is skipped.
* `description` — purpose (documentation, not rendered).
* `system` / `user` — Go `text/template` bodies rendered with the same data and returned separately.

The general "respond as JSON" directive lives in `system` (a behavioral contract); the exact schema
is repeated at the tail of `user` so it sits closest to the generation point (recency). `JSONMode` on
the LLM request remains the primary format enforcement.

## 5. Dependencies

* stdlib: `embed`, `text/template`, `io/fs`, `log/slog`.
* `gopkg.in/yaml.v3` — parsing blueprint YAML (same parser as `internal/config`).

## 6. Configuration

```yaml
prompt:
  dir: /etc/gruh/prompts   # empty value = built-in only
```

## 7. Errors and Edge Cases

* Broken template in a `critical` blueprint — fatal error at startup (with the blueprint name),
  so the operator notices instead of silently falling back.
* Broken template in a non-critical blueprint — `slog.Warn` and the blueprint is skipped.
* Unknown prompt name at `Execute` — error returned to the caller (classificator marks the event failed).
* A template referencing a non-existent field — render error propagated up.
* `dir` does not exist / is empty — not an error; run on built-in blueprints only.
* Escaping is not required (`text/template`, not `html/template`) — a prompt is plain text.

## 8. Testing

* Unit: built-in `classify` renders non-empty `system` and `user`; the user prompt carries the exact
  JSON schema and no leftover template syntax.
* Unit: override-by-name (a user file with `name: classify` replaces the built-in), filename fallback
  when `name` is omitted, and unknown-prompt error.
* Unit: a `critical` blueprint with a broken template makes `New` fail.

## 9. Open Questions and Accepted Decisions

* **Hot reload — removed**: blueprints are loaded once in `New`. Changes to the user directory take
  effect on restart. (Earlier the design accepted fsnotify-based hot-reload; it was dropped in favor
  of the simpler, proven load-once approach.)
* **Single-file blueprint — resolved**: `system` and `user` live in one YAML file per prompt; the
  previous split (`classify.md` + `classify_system.md`) and the front-matter format are gone.
* **Prompt versioning — resolved (YAML `version`)**: the `version` field is written to
  `prompt_version` in LLM traces instead of a content hash.
```
