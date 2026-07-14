# 07. Classificator (`internal/classificator`)

## 1. Purpose

The "brain" of the system: uses an LLM to decide whether an update is **important** (major release,
breaking changes, critical security patch) or **noise** (dev tags, RC, minor bumps).
A key design requirement: in addition to the current update, the model receives the context of
**the two most recent important updates** for that feed — to assess the scale of changes by comparison.

## 2. Responsibilities and Boundaries

**Does:**
* Assembles the LLM input: current event + history (2 most recent important updates).
* Renders the prompt via `internal/prompt`, calls `internal/llm`.
* Parses and validates the structured model response (JSON verdict).
* Returns the verdict with confidence and explanation.

**Does NOT:**
* Does not communicate with the LLM API directly (that is `llm`).
* Does not store verdicts and history (that is `storage`, via orchestrator).
* Does not send notifications.

## 3. Public Interface

```go
type Classifier interface {
	Classify(ctx context.Context, current UpdateEvent, history []ImportantUpdate) (Verdict, error)
}

type ImportantUpdate struct {
	Event      UpdateEvent
	Verdict    Verdict
	ClassifiedAt time.Time
}

type Verdict struct {
	Important  bool
	Category   string  // major_release | breaking_change | security | noise | ...
	Confidence float64 // 0..1
	Reason     string  // model explanation (for notification and debugging)
}
```

## 4. Internal Design

1. Data (current event + history) is substituted into the `classify.md` prompt template.
2. Request to LLM requiring a strict JSON response (structured output / json mode when supported by the API).
3. Response is parsed and validated: required fields, `Confidence` range, known `Category`.
4. On invalid response — up to N retries with the format error indicated in the prompt.
5. Importance threshold (accepted): `Important && Confidence >= confidence_threshold`,
   default **0.5** — when the model's confidence is below 50% the event is considered noise
   and is skipped (no notification sent); the verdict and confidence are stored
   in `storage` for analysis of false negatives (eval, phase 7).
6. Rule on top of LLM (accepted): **security patches are always important** — when
   `Category == "security"` the event is considered important regardless of `Confidence`
   (the threshold from point 5 does not apply): the cost of missing a security fix exceeds the cost
   of a false positive. The rule is duplicated as an instruction in the prompt.

Empty history (new feed, no important updates yet) — valid case: the prompt contains
a branch "no history, evaluate the update on its own".

**Telemetry:** for each `Classify` call a root OTEL span (trace in Langfuse) is created,
with inner spans for LLM requests (including format retries); `feed_url`,
`fingerprint`, prompt version, and the final verdict are written to metadata. Verdicts are also
counted in Prometheus (`gruh_classify_total{verdict=...}`). See [12-observability.md](12-observability.md).

## 5. Dependencies

* `internal/llm` — model client.
* `internal/prompt` — prompt templates.
* `internal/observability` — OTEL tracer (Langfuse) and classification metrics.

## 6. Configuration

```yaml
classificator:
  confidence_threshold: 0.5   # below 50% confidence — skip (accepted decision)
  max_format_retries: 2
  history_size: 2   # number of most recent important updates in context
```

## 7. Errors and Edge Cases

* LLM unavailable (after `internal/llm` retries) — **fail fast**: error propagated up
  and process crash without saving classification state — analogous to
  DB unavailability (see [08-llm.md](08-llm.md) §9, [06-orchestrator.md](06-orchestrator.md) §7).
* Invalid JSON after all retries — event is marked `classification_failed`, alert; NOT considered important by default (no spam), but logged for manual review.
* Content too long — truncated preserving the beginning and the end (that is usually where the changelog highlights are).
* Prompt injection in release notes — the prompt explicitly instructs the model to ignore instructions in data.

## 8. Testing

* Unit with a fake LLM client: response validation, format retries, confidence thresholds, empty history.
* Eval set (phase 7): labeled examples of real releases for regression quality evaluation of the prompt.

## 9. Open Questions and Accepted Decisions

* **Default on low confidence — resolved (stay silent)**: a confidence coefficient is introduced;
  at `Confidence < 0.5` the event is skipped (see §4, point 5).
* **Security patches — resolved (always important)**: at `Category == "security"` we notify
  regardless of `Confidence` — a rule on top of LLM (see §4, point 6).
* **LLM unavailability — resolved (fail fast)**: error and crash without saving
  classification state, analogous to DB unavailability (see §7).
