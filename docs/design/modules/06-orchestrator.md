# 06. Orchestrator (`internal/orchestrator`)

## 1. Purpose

Manages the event processing flow: connects pipeline stages in the correct order,
guarantees that the event passes through all required modules, and ensures
persistence of intermediate states. The only module that "knows" the full pipeline.

## 2. Responsibilities and Boundaries

**Does:**
* Defines the processing chain: fetch → parse → dedup → publish; consume → classify → store → dispatch.
* Subscribes to bus topics and calls the appropriate modules.
* Enriches `context.Context` with metadata (feed_id, trace_id) as the event moves.
* Assembles context for the classificator: queries `storage` for the two most recent important updates of the feed.
* Ensures processing can resume after a failure: event status is **derived from facts**
  in `storage` (`classified_at`, records in `dispatches`); no separate state machine is maintained (see §9).

**Does NOT:**
* Does not contain domain logic of the modules (fetch/parse/classify/notify) — only wiring.
* Does not implement transport (that is `bus`).

## 3. Public Interface

```go
type Orchestrator struct { /* dependencies via constructor */ }

func New(deps Deps) *Orchestrator

type Deps struct {
	Collector    collector.Collector
	Parser       parser.Parser
	Dedup        deduplicator.Deduplicator
	Bus          bus.Bus
	Classifier   classificator.Classifier
	Dispatcher   dispatcher.Dispatcher
	Store        storage.Store
}

// HandleFetchTask — handler for scheduler tasks (collector role).
func (o *Orchestrator) HandleFetchTask(ctx context.Context, task scheduler.FetchTask) error

// RunWorkers — subscriptions to bus topics (worker/dispatcher roles); blocks until ctx is cancelled.
func (o *Orchestrator) RunWorkers(ctx context.Context) error
```

## 4. Internal Design

### "Collection" flow (collector role)
1. `Collector.Fetch` → on `NotModified` finish.
2. `Parser.Parse` → events.
3. `Dedup.Filter` → new events only.
4. `Store.SaveUpdates` + `Bus.Publish("updates.new")` for each.

### "Classification" flow (worker role)
1. Subscribe to `updates.new`.
2. `Store.LastImportant(feedID, 2)` → context for LLM.
3. `Classifier.Classify(event, history)` → verdict.
4. `Store.SaveVerdict`; if important → `Bus.Publish("updates.important")`.

### "Delivery" flow (dispatcher role)
1. Subscribe to `updates.important`.
2. Resolve channels by the feed mapping, `Dispatcher.Send`.
3. `Store.MarkDispatched` (idempotency for repeated deliveries).

Roles are enabled via CLI flags: monolith runs all three, distributed mode runs one per process.

## 5. Dependencies

All pipeline modules (see `Deps`) — via interfaces only; orchestrator does not import implementations.

## 6. Configuration

```yaml
orchestrator:
  roles: [collector, worker, dispatcher]  # which roles are active in this process
  worker_concurrency: 4
```

## 7. Errors and Edge Cases

* Stage error → nack to the bus (redelivery); facts in `storage` (verdict, dispatch records)
  allow resuming from the point of failure without re-classifying.
* LLM or DB unavailable — **fail fast**: error propagated up and process crash without
  saving classification state (see [08-llm.md](08-llm.md) §9, [11-storage.md](11-storage.md) §9);
  after restart the event is reprocessed (idempotency via fingerprint and `dispatches`).
* Redelivery of an already-classified event — verdict is taken from `storage`, LLM is not called.
* Repeated notification sending — blocked by the `dispatched` flag in `storage`.
* Partial delivery failure (1 of N channels) — only the failed channel is retried.

## 8. Testing

* Unit with mocks of all `Deps`: call order, error handling at each stage, idempotency.
* Integration end-to-end: httptest feed + SQLite + memory bus + fake LLM → notification to a fake channel.

## 9. Open Questions and Accepted Decisions

* **State machine — resolved (option B, derive from facts)**: no explicit `status`
  table/column; event status is derived from existing data —
  `classified_at IS NOT NULL` → classified, record in `dispatches` → dispatched.
  Single source of truth, nothing to get out of sync, idempotency for free.
  A SQL view can be added for debugging visibility. Introduce an explicit state machine
  only if complex transitions appear (deferred classification, manual moderation).
* Does saga compensation need to happen on delivery failure after the verdict is recorded (or is retry sufficient)?
  Context: "saga" would mean rolling back/compensating already-completed steps (e.g., deleting
  the verdict if delivery ultimately fails). There is nothing to compensate here — the verdict
  is correct regardless of delivery outcome, and repeated sending is protected by `dispatches`
  idempotency. Recommendation: **sagas are not needed** — per-channel retry is sufficient
  (see [10-dispatcher.md](10-dispatcher.md) §4) + idempotency; a final delivery failure
  is recorded in the log/metric and is visible by the absence of a record in `dispatches`.
  Awaiting confirmation (phase 4).
