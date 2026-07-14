# 09. Prompt (`internal/prompt`)

## 1. Purpose

Prompt management for the LLM. Prompts are `.md` files with Go templates (`text/template`).
Built-in prompts are shipped in the binary via `go:embed`; the user can override
any of them with files in their own directory — **a user file takes priority over the built-in one**.

## 2. Responsibilities and Boundaries

**Does:**
* Loads prompts: user directory first, then falls back to `go:embed`.
* Renders templates with typed data (`text/template`).
* Validates templates at startup (all built-in + overridden ones must compile).
* **Hot-reload (accepted):** watches the user directory for changes (fsnotify)
  and reloads prompts without restarting the process (see §4).

**Does NOT:**
* Does not call the LLM and does not know the semantics of prompts.
* Does not edit prompts itself — only reads and renders.

## 3. Public Interface

```go
type Registry interface {
	// Render renders the prompt by name (e.g., "classify") with the given data.
	Render(name string, data any) (string, error)
}

func New(userDir string) (Registry, error) // userDir may be empty
```

## 4. Internal Design

### File Structure

```
internal/prompt/
├── prompt.go
├── registry.go
└── builtin/            # embedded via go:embed
    ├── classify.md         # main classification prompt
    └── classify_system.md  # system instruction
```

* `//go:embed builtin/*.md` → `embed.FS`.
* Name resolution: if `<userDir>/<name>.md` exists → use it; otherwise `builtin/<name>.md`.
* All templates are parsed once at `Registry` creation; errors are fatal at startup
  (fail fast, not at the first event).
* **Hot-reload (accepted):** `fsnotify` on `userDir`; when a file changes, the template is
  re-parsed on the side and atomically swapped in (`atomic.Pointer` on the template set).
  A broken new template is not applied: the previous version is kept + `error` log and metric.
* Data for `classify.md`: current update, slice of important update history, feed metadata.

### Prompt Header and Versioning (accepted decision)

Each prompt file begins with a **YAML header** (front matter, delimited by `---`):

```yaml
name: classify
version: "1.0.0"
critical: true
description: "Classify an update (...)"
```

* `name` — prompt name, must match the filename (`classify.md`) — otherwise a validation error.
* `version` — semantic version (semver); this is what is written into LLM traces
  (`prompt_version`, see [12-observability.md](12-observability.md)) and used in eval (phase 7).
* `critical` — the prompt is required for pipeline operation: missing/broken file — fail fast
  at startup; for non-critical ones — warning and fallback to built-in.
* `description` — prompt purpose (documentation, not included in rendering).

The header is stripped during loading — only the template body is sent to the LLM. A missing header
or missing required fields (`name`, `version`) — validation error at startup/hot-reload.
When overriding a built-in prompt, the user must specify their own version —
this way traces show exactly which prompt variant produced the verdict.

### Template Example (`classify.md`)

```markdown
---
name: classify
version: "1.0.0"
critical: true
description: "Classify an update against the feed's recent important updates"
---
Assess the importance of the project update.

## Current Update
{{ .Current.RawContent }}

## Most Recent Important Updates (for comparison)
{{ range .History }}- {{ .Event.PublishedAt }}: {{ .Verdict.Reason }}
{{ else }}No important update history.
{{ end }}

Reply strictly as JSON: {"important": bool, "category": string, "confidence": float, "reason": string}
```

## 5. Dependencies

* stdlib: `embed`, `text/template`, `io/fs`.
* `github.com/fsnotify/fsnotify` — hot-reload of user prompts.
* YAML parser (same one as in `internal/config`) — for parsing the prompt header.

## 6. Configuration

```yaml
prompt:
  user_dir: /etc/gruh/prompts   # empty value = built-in only
  hot_reload: true              # fsnotify on user_dir (accepted decision)
```

## 7. Errors and Edge Cases

* Broken user template — error at startup with filename and position (not a silent fallback to built-in, so the user notices).
* Missing/invalid YAML header, `name` mismatch with filename,
  non-semver `version` — validation error (for `critical: true` — fail fast).
* User file references a non-existent field — render error propagated up (classificator marks the event as failed).
* `user_dir` does not exist — not an error, run on built-in prompts, warning in the log.
* Escaping is not required (`text/template`, not `html/template`) — a prompt is plain text.

## 8. Testing

* Unit: priority of user files over built-in, fallback, parse/render errors.
* Unit: YAML header parsing (valid/missing/broken, `name` mismatch,
  non-semver `version`), stripping the header from the body before rendering.
* Golden render tests of `classify.md` on fixed data (protection against accidental prompt edits).

## 9. Open Questions and Accepted Decisions

* **Hot reload — resolved (yes)**: fsnotify on `user_dir`, atomic template set swap,
  broken templates are not applied (see §4).
* **Prompt versioning — resolved (YAML header)**: each file starts with
  front matter (`name`, `version` (semver), `critical`, `description`, see §4);
  the `version` from the header is written to `prompt_version` in LLM traces
  instead of a content hash.
