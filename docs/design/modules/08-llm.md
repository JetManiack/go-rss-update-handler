# 08. LLM (`internal/llm`)

## 1. Purpose

A thin transport client for the **OpenAI-compatible API** (OpenAI, Azure OpenAI, Ollama,
vLLM, OpenRouter, etc.). Isolates the rest of the system from the specifics of a particular provider.

## 2. Responsibilities and Boundaries

**Does:**
* Chat completions requests (`POST /v1/chat/completions`) with JSON mode support.
* Timeouts, retries with backoff on 429/5xx, respects `Retry-After`.
* Token usage accounting (metrics: prompt/completion tokens, latency, cost).
* Instrumentation: Prometheus metrics (`gruh_llm_*`) and OTEL spans on each request
  with GenAI attributes (exported to Langfuse) — see [12-observability.md](12-observability.md).
* Concurrent request limiting (semaphore) — protects budget and provider rate limits.

**Does NOT:**
* Does not build prompts or interpret responses (that is `classificator` + `prompt`).
* Does not cache responses (possible future development, phase 7).

## 3. Public Interface

```go
type Client interface {
	Complete(ctx context.Context, req Request) (Response, error)
}

type Request struct {
	System      string
	User        string
	JSONMode    bool    // require application/json response
	MaxTokens   int
	Temperature float64
}

type Response struct {
	Content          string
	PromptTokens     int
	CompletionTokens int
	Model            string
}

func New(cfg Config) Client
```

## 4. Internal Design

* **Implementation over `net/http` (accepted, see §9)**: provider is vLLM/LiteLLM,
  only chat completions + JSON mode are needed; the OpenAI-compatible API format is stable and simple.
* `base_url` is configurable → works with any compatible provider, including local ones.
* Retries: only idempotent errors (network, 429, 5xx); 4xx (except 429) — fatal immediately.
* API key — from environment variable only (`GRUH_LLM_API_KEY`), not stored in config.

## 5. Dependencies

* stdlib `net/http`, `encoding/json` — no external LLM SDK (see §9).
* `internal/observability` — metrics and OTEL tracer for LLM telemetry (Langfuse).

## 6. Configuration

```yaml
llm:
  base_url: http://litellm.internal:4000/v1   # vLLM / LiteLLM (OpenAI-compatible endpoint)
  model: gpt-4o-mini
  timeout: 60s
  max_retries: 3
  max_concurrent: 4
  temperature: 0.1     # classification — low temperature
# api key: env GRUH_LLM_API_KEY
```

## 7. Errors and Edge Cases

* 429 / quota exceeded — backoff respecting `Retry-After`; after retries exhausted —
  error propagated up and **fail fast**: process crashes without saving classification state
  (see §9 and [06-orchestrator.md](06-orchestrator.md) §7).
* Model context exceeded (`context_length_exceeded`) — specialized error, on which the classificator will truncate its input.
* Connection drop / timeout — retry.
* Response with no `choices` or with `finish_reason: length` — error with diagnostics.

## 8. Testing

* Unit with `httptest.Server` mimicking the OpenAI API: success, 429 with Retry-After, 5xx, malformed JSON, JSON mode.
* Optional smoke test against a real provider behind a build tag (`//go:build llm_live`).

## 9. Open Questions and Accepted Decisions

* **`net/http` vs `openai-go` — resolved: custom implementation on `net/http`.**
  Rationale for vLLM/LiteLLM providers:
  * only one endpoint is used (`/v1/chat/completions` + JSON mode) — that is ~200 lines of code,
    a full SDK is overkill;
  * `openai-go` evolves along with the cloud OpenAI API (Responses API, new fields) —
    OpenAI-compatible proxies often lag behind in supporting new fields/strict validation;
    the thin client sends exactly what they understand;
  * full control over retries/timeouts/semaphore and OTEL instrumentation
    without wrappers around someone else's transport;
  * fewer dependencies, trivial testing via `httptest.Server`.
  If streaming/tools are needed — the decision can be revisited; the `Client` interface allows it.
* **Fallback between models — resolved (not needed in the application)**: if needed,
  fallback is configured on the LiteLLM proxy side. If the model is unavailable
  (after retries exhausted) — **error and crash without saving classification state**
  — analogous to DB unavailability (fail fast,
  see [11-storage.md](11-storage.md) §9); after restart events are reprocessed.
