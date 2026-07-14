# 04. Deduplicator (`internal/deduplicator`)

## 1. Purpose

Guarantees that each update is processed exactly once. Computes a stable **fingerprint**
for an event and filters out already-known updates before they enter the bus and reach
the expensive LLM classification.

## 2. Responsibilities and Boundaries

**Does:**
* Computes an event fingerprint from stable fields.
* Checks the fingerprint against the store of known updates.
* Registers new fingerprints (atomically, to prevent races).

**Does NOT:**
* Does not store data itself — uses `storage` (and optionally a Redis cache).
* Does not decide whether an update is important.

## 3. Public Interface

```go
type Deduplicator interface {
	// Filter returns only new events, filling in their Fingerprint
	// and atomically registering them as known.
	Filter(ctx context.Context, events []UpdateEvent) ([]UpdateEvent, error)
}

// Fingerprint computes a stable fingerprint for an event.
func Fingerprint(e UpdateEvent) string
```

## 4. Internal Design

* **Fingerprint formula:** `sha256(sourceURL + "\n" + guid|link + "\n" + publishedAt.UTC())`,
  hex representation. Content is not included in the hash: edits to release notes must not
  create a "new" event (otherwise — duplicate notifications).
* **Uniqueness check:** unique index on `fingerprint` in the `updates` table +
  `INSERT ... ON CONFLICT DO NOTHING` — atomic even with multiple instances.
* **Cache (optional, phase 5):** Redis `SET NX` with TTL as a fast first level before hitting the DB.

## 5. Dependencies

* `internal/storage` — the known updates table.
* Redis (optional, distributed mode).
* stdlib `crypto/sha256`.

## 6. Configuration

```yaml
deduplicator:
  cache_ttl: 720h   # TTL of records in the Redis cache (if enabled)
  use_cache: false
```

## 7. Errors and Edge Cases

* Race between two instances on the same event — resolved by the DB unique index; the loser
  gets a "duplicate" and silently skips the event.
* `PublishedAt` change on an existing feed entry (GitHub does this when editing a release)
  — will produce a new fingerprint; acceptable trade-off, documented as known behavior.
* DB unavailable — **fail fast (accepted)**: typed error propagated up, the application
  logs the error and exits with a non-zero code (DB is a mandatory dependency;
  in k8s the pod orchestrator handles restarts, and unprocessed feeds will be polled again).
* Entry with no guid and no link — fallback to content hash.

## 8. Testing

* Unit: fingerprint stability (one event → one hash), sensitivity to each field in the formula.
* Integration (SQLite): concurrent registration of the same event from multiple goroutines.

## 9. Open Questions and Accepted Decisions

* **Policy on storage unavailability — resolved (fail fast)**: the application returns
  an error and crashes; correct deduplication is impossible without the DB, and "silent"
  degradation leads either to lost events or to duplicate notifications (see §7).
* Does old fingerprint cleanup (retention) need to happen, or do we store forever?
  See the retention discussion in [11-storage.md](11-storage.md) §9.
