# 12. Observability (`internal/observability`)

## 1. Purpose

A cross-cutting application observability layer. Three components:

* **Logging** — structured logs via the standard `log/slog`.
* **Application telemetry** — **Prometheus** metrics (`/metrics` endpoint) across all pipeline modules.
* **LLM telemetry** — tracing of LLM calls via **Langfuse** on top of **OpenTelemetry (OTEL)**:
  Langfuse accepts traces via the standard OTLP protocol, so `internal/llm` is instrumented
  with the regular OTEL SDK, and Langfuse acts as the OTLP backend (generations, prompts, tokens, cost).

Like `storage`, this module is not a pipeline step — it is used by all other modules.

## 2. Responsibilities and Boundaries

**Does:**
* Initialization and configuration of `slog` (level, text/JSON format, output).
* Enriching logs with event context (`feed_url`, `fingerprint`, `trace_id`) via a `slog.Handler`
  that reads attributes from `context.Context`.
* Registration and export of Prometheus metrics; HTTP endpoint `/metrics` (+ `/healthz`, `/readyz`).
* Initialization of the OTEL `TracerProvider` with an OTLP exporter pointing to Langfuse;
  graceful flush/shutdown of the provider.
* Helpers for creating LLM spans with Langfuse semantics (GenAI semantic conventions:
  `gen_ai.prompt`, `gen_ai.completion`, `gen_ai.usage.*`).

**Does NOT:**
* Does not decide *what* to log/measure — that is the responsibility of each module.
* Does not store or aggregate metrics (that is the Prometheus server) or traces (that is Langfuse).
* Does not handle alerting (Alertmanager/Grafana — outside the application).

## 3. Public Interface

```go
// Init initializes the full observability layer from config.
func Init(ctx context.Context, cfg Config) (*Observability, error)

type Observability struct { /* ... */ }

// Logger — root slog.Logger; modules receive it via DI and do logger.With("module", "collector").
func (o *Observability) Logger() *slog.Logger

// Tracer — OTEL tracer for LLM instrumentation (exported to Langfuse via OTLP).
func (o *Observability) Tracer() trace.Tracer

// Handler — HTTP handler for /metrics + probes; mounted in cmd/gruh.
func (o *Observability) Handler() http.Handler

// Shutdown — flush OTEL spans and graceful shutdown.
func (o *Observability) Shutdown(ctx context.Context) error

// Contextual log attributes: added once, appear in all log records further down the stack.
func WithLogAttrs(ctx context.Context, attrs ...slog.Attr) context.Context
```

Module metrics are regular `prometheus.Counter/Histogram/Gauge`, registered in a shared
`prometheus.Registerer` that is passed to modules at application assembly.

## 4. Internal Design

### 4.1 Logging (slog)

* One root `*slog.Logger`, format configured by config: `text` (local development) / `json` (production).
* Each module receives the logger via DI and adds `slog.With("module", "<name>")`.
* A custom `slog.Handler` decorator extracts attributes from `context.Context`
  (placed there by `WithLogAttrs`) — so `feed_url`/`fingerprint`/`trace_id` appear in all event
  processing logs without manual threading.
* Levels: `debug` — HTTP/LLM request details; `info` — event lifecycle
  (fetched, deduplicated, classified, dispatched); `warn` — retries, degradations;
  `error` — event loss, errors after all retries.
* Secrets (API keys, channel tokens) never appear in logs; feed bodies and prompts — only at `debug`.

### 4.2 Application Metrics (Prometheus)

Export via `prometheus/client_golang`, endpoint `/metrics` on a separate port (not public-facing).

Base set of metrics per module (prefix `gruh_`):

| Metric | Type | Labels | Module |
|---------|-----|--------|--------|
| `gruh_scheduler_ticks_total` | Counter | `feed` | scheduler |
| `gruh_collector_fetch_total` | Counter | `feed`, `status` (ok/not_modified/error) | collector |
| `gruh_collector_fetch_duration_seconds` | Histogram | `feed` | collector |
| `gruh_parser_items_total` | Counter | `format` | parser |
| `gruh_dedup_events_total` | Counter | `result` (new/duplicate) | deduplicator |
| `gruh_bus_events_total` | Counter | `topic`, `result` (ok/retry/dlq) | bus |
| `gruh_bus_queue_depth` | Gauge | `topic` | bus |
| `gruh_classify_total` | Counter | `verdict` (important/noise/failed) | classificator |
| `gruh_llm_requests_total` | Counter | `model`, `status` | llm |
| `gruh_llm_request_duration_seconds` | Histogram | `model` | llm |
| `gruh_llm_tokens_total` | Counter | `model`, `kind` (prompt/completion) | llm |
| `gruh_dispatch_total` | Counter | `channel_type`, `status` | dispatcher |

Cardinality of the `feed` label is controlled (the number of feeds is finite and defined by config);
if the number of feeds grows, the label is replaced with an aggregate.

### 4.3 LLM Telemetry (Langfuse + OTEL)

* The `classificator` → `llm` pair is instrumented: a root span (trace) is created for each
  classification; inside — spans for LLM requests (including format retries).
* Export — standard **OTLP/HTTP** exporter of the OTEL SDK, endpoint is a **self-hosted** Langfuse
  instance (`https://<langfuse-host>/api/public/otel`; the cloud cloud.langfuse.com is not used),
  authentication — Basic Auth from `public_key`/`secret_key` in the `Authorization` header.
* Spans follow **OTEL GenAI semantic conventions**, which Langfuse understands natively:
  * `gen_ai.system`, `gen_ai.request.model`, `gen_ai.request.temperature`;
  * `gen_ai.prompt` / `gen_ai.completion` (full texts — only if `capture_content: true`);
  * `gen_ai.usage.prompt_tokens` / `gen_ai.usage.completion_tokens`.
* Trace metadata: `feed_url`, `fingerprint`, prompt version (`prompt_version` — `version`
  from the YAML header of the prompt, see [09-prompt.md](09-prompt.md) §4) — to
  correlate verdict quality with a specific version of `classify.md` and build an eval set (phase 7).
* `trace_id` is written to logs (see 4.1) — cross-cutting "log ↔ trace" correlation.
* Export is asynchronous (`BatchSpanProcessor`); Langfuse unavailability does not affect the pipeline.

## 5. Dependencies

* stdlib `log/slog`, `net/http`.
* `github.com/prometheus/client_golang` — metrics.
* `go.opentelemetry.io/otel` + `otlptracehttp` — LLM tracing (export to Langfuse).

## 6. Configuration

```yaml
observability:
  log:
    level: info          # debug | info | warn | error
    format: json         # text | json
  metrics:
    enabled: true
    listen: ":9090"      # separate port for /metrics, /healthz, /readyz
  llm_telemetry:         # Langfuse (self-hosted) via OTEL
    enabled: true
    otlp_endpoint: https://langfuse.internal.example.com/api/public/otel  # self-hosted instance only
    capture_content: false   # whether to write full prompts/responses to traces
    sample_rate: 1.0
# Langfuse keys: env GRUH_LANGFUSE_PUBLIC_KEY / GRUH_LANGFUSE_SECRET_KEY
```

## 7. Errors and Edge Cases

* Observability never crashes the pipeline: metric/trace export errors are logged at `warn`
  and are not propagated up.
* Langfuse unavailable — `BatchSpanProcessor` buffers and drops on overflow; dropped span
  count is tracked in metrics.
* `llm_telemetry.enabled: false` or missing keys — no-op `TracerProvider`, LLM runs without tracing.
* Secret leakage: prompts/responses end up in traces only with explicit `capture_content: true`;
  API keys and tokens are redacted at the logging helper level.
* Shutdown: `Shutdown()` with a timeout flushes unsent spans when the process stops.

## 8. Testing

* Unit: contextual `slog.Handler` (attributes from `ctx` appear in records), secret filtering.
* Unit: metric registration without conflicts, correct increments via `prometheus/testutil`.
* Unit: LLM spans via `tracetest.InMemoryExporter` — correct GenAI attributes, token accounting,
  nesting of retry spans, absence of content when `capture_content: false`.
* Integration (optional, behind a build tag): sending a test trace to a self-hosted Langfuse.

## 9. Open Questions and Accepted Decisions

* **Langfuse Go SDK vs pure OTEL — resolved (pure OTEL + OTLP)**: vendor-neutral
  standard, no SDK lock-in; Langfuse scores/prompt management are not needed at this stage.
* **`sample_rate` — resolved (yes)**: the `llm_telemetry.sample_rate` parameter is in the config
  with a default of `1.0` (all traces); reduced when volume/storage cost grows.
  Sampling is at the root classification span level (`TraceIDRatioBased`),
  so a trace is either kept or dropped entirely.
* **Tracing boundaries — resolved (LLM only, for Langfuse only)**: OTEL tracing is
  limited to the `classificator` → `llm` pair with export to a self-hosted Langfuse (see §4.3);
  it is not extended to the rest of the pipeline — observability of other stages
  is provided by Prometheus metrics and logs with `trace_id` correlation.
