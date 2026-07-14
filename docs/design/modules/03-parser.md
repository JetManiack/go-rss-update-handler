# 03. Parser (`internal/parser`)

## 1. Purpose

Converts raw feed content (RSS 2.0 / Atom / JSON Feed) into a unified internal
format — a list of `UpdateEvent`. Hides format differences from the rest of the pipeline.

## 2. Responsibilities and Boundaries

**Does:**
* Auto-detects format and parses via `gofeed`.
* Maps feed entries to `UpdateEvent` (SourceURL, RawContent, PublishedAt).
* Normalizes: time zones → UTC, HTML content cleaning/truncation, selection of the most
  informative field (content > description > title).

**Does NOT:**
* Does not check uniqueness (that is `deduplicator`).
* Does not compute fingerprints (that is `deduplicator`), but provides stable fields for them.
* Does not make network requests.

## 3. Public Interface

```go
type Parser interface {
	// Parse parses the raw feed body and returns events in publication order (newest first).
	Parse(ctx context.Context, feedURL string, body []byte) ([]UpdateEvent, error)
}

// UpdateEvent — the core data model of the system; defined in the `internal/model` package
// (accepted decision, see §9) and used by parser, bus, deduplicator, storage.
type UpdateEvent struct {
	SourceURL   string    // URL of the entry (link) or the feed URL
	RawContent  string    // normalized entry content
	PublishedAt time.Time // UTC
	Fingerprint string    // filled in by the deduplicator
}
```

## 4. Internal Design

* `gofeed.Parser` with `ParseString`/`Parse` — universal for RSS/Atom/JSON.
* For GitHub Atom: entry = release/tag; `SourceURL` = `entry.Link`, content = `entry.Content`
  (release notes in HTML), the title contains the tag name.
* Date fallback: `Published` → `Updated` → fetch time (with a note).
* `RawContent` size limit (e.g., 64 KiB) to protect LLM context and the DB.

## 5. Dependencies

* `github.com/mmcdole/gofeed`.
* `internal/model` — shared `UpdateEvent` type (accepted decision, see §9).

## 6. Configuration

```yaml
parser:
  max_content_size: 64KiB
  strip_html: false   # whether to keep HTML in RawContent (LLM handles HTML/markdown well)
```

## 7. Errors and Edge Cases

* Invalid XML/JSON — parse error propagated up; feed is marked problematic after N consecutive failures.
* Empty feed (no entries) — not an error, empty result.
* Entries without dates/links — filled with fallback values; event is not lost.
* Non-standard encodings — gofeed converts; on failure — error with diagnostics.
* Duplicate entries within a single feed — return all; `deduplicator` will filter them out.

## 8. Testing

* Golden files: real examples of GitHub Atom, RSS 2.0, JSON Feed in `testdata/`.
* Edge cases: empty feed, broken XML, missing dates, huge content.

## 9. Open Questions and Accepted Decisions

* **Location of `UpdateEvent` — resolved (`internal/model`)**: the type is needed by several
  modules at once (parser, bus, deduplicator, storage, dispatcher), so it lives in a
  dedicated `internal/model` package with no dependencies — no module needs to import parser
  just for the type.
* **GitHub semver tag — resolved (classificator's responsibility)**: the parser does not extract
  structured GitHub fields; the title/content with the tag name end up in `RawContent` as-is;
  interpreting the version (major/minor/patch, security) is the task of LLM classification.
