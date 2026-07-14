# 01. Scheduler (`internal/scheduler`)

## 1. Purpose

The scheduler is the entry point of the pull model. It generates polling tasks for each feed
at that feed's individual interval and passes them to the collector. It applies **jitter** to
avoid load spikes (when all feeds are polled simultaneously) and to respect source rate limits.

## 2. Responsibilities and Boundaries

**Does:**
* Maintains the polling schedule for each feed: the feed list comes from the DB (source of truth),
  intervals come from the config (global `default_interval`), see [13-config.md](13-config.md).
* Adds random jitter to the task start time.
* Publishes a `FetchTask{FeedID, URL}` for the collector.
* **Adaptive polling (accepted):** takes into account the result of the previous poll — on repeated 304s/errors
  the feed's interval gradually increases (up to `max_backoff × default_interval`); on new
  content it resets to the base. Backoff state is kept in process memory (not in the DB).

**Does NOT:**
* Does not make HTTP requests (that is `collector`).
* Does not know about feed content or its importance.

## 3. Public Interface

```go
type Scheduler interface {
	// Run blocks until ctx is cancelled; calls handler on each feed tick.
	Run(ctx context.Context) error
}

type FetchTask struct {
	FeedID int64
	URL    string
}

type TaskHandler func(ctx context.Context, task FetchTask) error

func New(feeds FeedSource, handler TaskHandler, opts ...Option) Scheduler
```

`FeedSource` — an abstraction over `storage` for obtaining the current feed list and their intervals.

## 4. Internal Design

* Each feed has its own timer (`time.Timer`), recreated after every firing:
  `nextRun = interval + rand(-jitter, +jitter)`.
* The feed list is periodically re-read from `FeedSource` (hot reload when feeds are added/removed).
* Parallelism limit: worker pool / semaphore on the number of concurrent tasks.
* In distributed mode (multiple replicas) — distributed lock per feed
  (Redis `SETNX` with TTL) so that a feed is not polled twice.

## 5. Dependencies

* `internal/storage` (via the `FeedSource` interface).
* Redis (only in multi-replica mode, via `internal/bus` or a separate lock client).

## 6. Configuration

```yaml
scheduler:
  default_interval: 15m   # base polling interval (intervals are in config only, not in DB)
  jitter: 20%             # fraction of the interval added randomly
  max_concurrent: 10      # concurrent polling tasks
  reload_interval: 1m     # period for re-reading the feed list from the DB
  adaptive:
    enabled: true         # adaptive polling (accepted decision)
    max_backoff: 8        # max multiplier to default_interval for "quiet" feeds
```

## 7. Errors and Edge Cases

* A task handler error does not stop the scheduler — the feed is rescheduled for the next interval.
* Long poll: a new tick for a feed does not start until the previous one is complete (skip, not queue).
* Empty feed list — the scheduler idles and waits for a reload.
* Shutdown: cancelling `ctx` stops all timers and waits for active tasks (graceful shutdown).

## 8. Testing

* Unit: deterministic jitter via `rand.Source` injection; fake clock (a `Clock` interface).
* Verification: interval compliance, absence of concurrent polling of the same feed, graceful shutdown.

## 9. Accepted Decisions (formerly open questions)

* **Adaptive polling — yes**: backoff on inactive feeds is part of the design (see §2/§6);
  implemented in phase 2 together with the scheduler.
* **Source of truth**: feed list — DB; intervals — config only (global values,
  no per-feed intervals in the DB). See [13-config.md](13-config.md).
