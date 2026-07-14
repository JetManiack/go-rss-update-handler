# 02. Collector (`internal/collector`)

## 1. Purpose

Downloads the raw content of a feed over HTTP based on a task from the scheduler. Must use
conditional requests (`ETag` / `If-Modified-Since`) — critical for GitHub, which actively
supports 304 responses and rate limits.

## 2. Responsibilities and Boundaries

**Does:**
* HTTP GET with conditional headers (`If-None-Match: <etag>`, `If-Modified-Since`).
* Stores/updates `ETag` and `Last-Modified` per feed (via `storage`).
* Per-host rate limiting (e.g., `golang.org/x/time/rate`).
* Retry with exponential backoff on network errors and 5xx; respects `Retry-After` on 429.
* Returns raw content (`[]byte`) and response metadata.

**Does NOT:**
* Does not parse content (that is `parser`).
* Does not decide when to poll (that is `scheduler`).

## 3. Public Interface

```go
type Collector interface {
	// Fetch returns the fetch result; NotModified == true on 304.
	Fetch(ctx context.Context, feed FeedRef) (FetchResult, error)
}

type FeedRef struct {
	FeedID       int64
	URL          string
	ETag         string
	LastModified string
}

type FetchResult struct {
	NotModified  bool
	Body         []byte
	ETag         string
	LastModified string
	FetchedAt    time.Time
}
```

## 4. Internal Design

* One reusable `http.Client` with timeouts (connect, total) and a response body size limit.
* Per-host `rate.Limiter` stored in a map with a mutex (host is extracted from the URL).
* Custom `User-Agent` (`gruh/<version>`), redirect support with a limit.
* Conditional headers: if the server returns 304 — result is `NotModified`, body is not read;
  new ETag/Last-Modified values, if present, are saved.

## 5. Dependencies

* `internal/storage` — ETag/Last-Modified persistence (via the caller or an interface).
* stdlib `net/http`, `golang.org/x/time/rate`.

## 6. Configuration

```yaml
collector:
  timeout: 30s
  max_body_size: 5MiB
  user_agent: "gruh/1.0"
  rate_limit_per_host: 1rps
  retries: 3
  backoff_base: 2s
```

## 7. Errors and Edge Cases

* `429 Too Many Requests` — respect `Retry-After`, reduce frequency (signal to the scheduler).
* `404/410` — feed is dead: mark in storage, notify (do not retry indefinitely).
* Oversized body / non-XML content — error with diagnostics, without crashing the process.
* 301 redirects — optionally update the feed URL in storage.
* Connection interrupted mid-body — retry with backoff.

## 8. Testing

* Unit with `httptest.Server`: scenarios 200/304/404/429/5xx, verification of conditional headers.
* Verification of rate limiter (multiple URLs on the same host), body size limit, retries.

## 9. Accepted Decisions (formerly open questions)

* **Signal for adaptive polling — yes**: adaptive polling is accepted (see [01-scheduler.md](01-scheduler.md) §2);
  collector returns the poll result (`NotModified`/error/new content) in `FetchResult`,
  and the scheduler uses it for backoff on "quiet" feeds.
* **HTTP proxy / custom CA — no**: not supported and not planned
  (the system CA pool and direct connection are used).
