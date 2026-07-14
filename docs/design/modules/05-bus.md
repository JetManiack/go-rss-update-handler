# 05. Bus (`internal/bus`)

## 1. Purpose

A unified event bus connecting the pipeline stages. Allows running the system as a monolith
(in-memory) or in distributed mode (Redis), decoupling `collector` from `classificator` and `dispatcher`
for independent scaling in k8s.

## 2. Responsibilities and Boundaries

**Does:**
* Publish/subscribe on typed topics (`updates.new`, `updates.classified`, ...).
* At-least-once delivery guarantee (in Redis mode: ack, redelivery, DLQ).
* Event serialization + transfer of enrichment metadata (values from `context.Context`
  are explicitly packed into the message envelope — the context itself is not serialized).

**Does NOT:**
* Does not contain routing business logic (that is `orchestrator`).
* Does not handle deduplication (at-least-once is compensated by consumer idempotency).

## 3. Public Interface

```go
type Bus interface {
	Publish(ctx context.Context, topic string, msg Message) error
	// Subscribe blocks until ctx is cancelled; handler must be idempotent.
	Subscribe(ctx context.Context, topic, group string, handler Handler) error
}

type Message struct {
	ID       string            // uuid, for tracing
	Version  int               // envelope schema version (see §9)
	Event    UpdateEvent       // shared type from internal/model (see 03-parser.md §9)
	Metadata map[string]string // enrichment (feed_id, verdict, trace_id, ...)
}

type Handler func(ctx context.Context, msg Message) error
```

An error from `Handler` = nack: the message will be redelivered (Redis mode) or logged (in-memory).

## 4. Internal Design

### Implementations

| Implementation | Mechanism | Usage |
|-----------|----------|----------------|
| `memorybus` | buffered Go channels | monolith, tests |
| `redisbus` | Redis Streams + consumer groups (`XADD`/`XREADGROUP`/`XACK`) | distributed mode |

### Redis Mode
* Each topic is a separate stream; `group` is a consumer group (scaling consumers).
* `XAUTOCLAIM` to reclaim messages from dead consumers.
* After N failed deliveries the message goes to the DLQ stream `<topic>.dlq`.
* Serialization: JSON (simple and debuggable; switch to msgpack if needed).

### Enrichment via context
Within a single process, enrichment is passed via `context.Context`; at the bus boundary,
values are explicitly copied into `Message.Metadata` and restored to the context on the consumer side.

## 5. Dependencies

* `github.com/redis/go-redis/v9` (only `redisbus`).

## 6. Configuration

```yaml
bus:
  driver: memory        # memory | redis
  redis:
    addr: localhost:6379
    stream_max_len: 10000
    max_deliveries: 5   # after this — to DLQ
    block_timeout: 5s
```

## 7. Errors and Edge Cases

* Redis unavailable — `Publish` returns an error with retry on the caller side; subscribers reconnect with backoff.
* Stream overflow — `MAXLEN ~` trims old messages (loss metric is mandatory).
* Redelivery — normal (at-least-once); consumers must be idempotent.
* "Poison" message (handler fails permanently) — goes to DLQ, operator alert.

## 8. Testing

* A contract test suite run on both implementations (memory and redis via testcontainers/miniredis).
* Scenarios: delivery, ack, redelivery after consumer failure, DLQ, concurrent consumers in a group.

## 9. Open Questions and Accepted Decisions

* **Topic list and versioning — resolved (base set + version in envelope)**:
  there are three topics — at the pipeline stage boundaries:
  * `updates.new` — new event after deduplication (collector side → worker);
  * `updates.classified` — event with "important" verdict (worker → dispatcher);
  * `<topic>.dlq` — DLQ for each topic (see §4).
  The list consists of constants in the `bus` package (not inline strings at call sites). Versioning —
  the `Version` field in the `Message` envelope (not in the topic name): consumers read
  old and new versions (tolerant JSON unmarshal, new fields are only ever added);
  topics like `updates.new.v2` are introduced only for incompatible breaking changes
  (relevant only in distributed mode with rolling deploys, phase 5).
* **Retry topic for LLM — resolved (not needed)**: when the LLM is unavailable the application
  crashes (fail fast, see [08-llm.md](08-llm.md) §9) — deferred retries inside the bus
  are pointless; after restart events are reprocessed (idempotency via fingerprint and `dispatches`).
