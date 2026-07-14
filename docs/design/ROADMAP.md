# GRUH — Development Roadmap

The roadmap is divided into phases; each phase concludes with a working increment.
Module implementation order is chosen to achieve an end-to-end vertical slice (feed → parsing → deduplication → notification) as early as possible, while LLM classification and distribution are added on top of a stable core.

## Development Process

All phases are implemented following a unified process:

* **TDD** — each feature starts with a failing test: red → green → refactor.
  A checklist item is considered complete only when there are tests covering its behavior.
* **GitFlow (feature branches)** — for every feature (usually one checklist item), a separate branch `feature/<short-name>` is created from `main`. After passing all tests (locally and in CI), the branch is merged into `main`. `main` is always in a green state.
* **Definition of Done** — a branch is not considered complete and is not merged into `main` until `make test`, `make lint`, `make security`, and `make build` pass without errors locally (see `Makefile` and `.junie/guidelines.md`).
* **Progress Tracking** — after implementation and merging of a checklist item, its checkbox is marked as `[x]` in this file (see `.junie/guidelines.md`).

## Phase 0 — Project Skeleton (completed)

**Goal:** buildable binary and development infrastructure.

> Prerequisite: git repository is initialized by the project owner before development starts
> (`git init`, first commit with documentation, `main` branch).

- [x] Module initialization, directory layout (`cmd/gruh`, `internal/*`, `deploy/`, `docs/`)
- [x] CLI skeleton based on `urfave/cli/v3`: **a single root command** `gruh` = service start
  (serve functionality), flags `--config`, `--version`, and `--check-config` (config validation
  without execution); no separate `serve`/`migrate`/`version` commands,
  subcommands are reserved for microservice roles (Phase 5)
- [x] `internal/config`: configuration loading (koanf: YAML + env, env priority), fail-fast validation, `config.example.yaml`
- [x] `internal/observability` (base part): logging `log/slog` (levels, text/JSON, contextual attributes), graceful shutdown on signals
- [x] `Makefile` (build, lint, test), `golangci-lint`, CI — **GitHub Actions** (`.github/workflows/ci.yml`: build + lint + `go test ./...` on PR and `main`)
- [ ] `docker-compose` for local development (PostgreSQL, Redis)

**Documents:** [12-observability.md](modules/12-observability.md), [13-config.md](modules/13-config.md)

**Output:** `gruh --version` works, environment spins up with one command.

## Phase 1 — Storage and Data Model (completed)

**Goal:** persistence layer that all other modules rely on.

- [x] `internal/storage`: GORM models (Feed, Update, RawContent, Channel, FeedChannelMapping, Dispatch);
  fingerprint/verdict stored forever, raw content in `raw_contents` with retention policy
- [x] Repositories + migrations (AutoMigrate / versioned) — executed automatically
  on root command startup (fail fast on error), no separate `migrate` command
- [x] PostgreSQL (prod) and SQLite (local/tests) support
- [x] DB is the source of truth for feeds/channels/mapping; management is currently done directly in DB
  (seed/SQL scripts); management transports (Slack/Telegram bot) — separate step (Phase 7);
  polling intervals in config

**Documents:** [11-storage.md](modules/11-storage.md), [13-config.md](modules/13-config.md)

## Phase 2 — Collection and Parsing (vertical slice without LLM) (completed)

**Goal:** the system polls feeds and stores new unique updates in the DB.

- [x] `internal/scheduler`: interval scheduler with jitter
- [x] `internal/collector`: HTTP client with `ETag` / `If-Modified-Since`, rate limiting, retry/backoff
- [x] `internal/model`: common `UpdateEvent` type (decision made, see [03-parser.md](modules/03-parser.md) §9)
- [x] `internal/parser`: gofeed → unified `UpdateEvent` (semver tag not extracted — this is the classificator's zone)
- [x] `internal/deduplicator`: fingerprinting, deduplication
- [x] In-memory `internal/bus` implementation (bus interface is fixed here;
  topic constants `updates.new` / `updates.classified`, schema version in `Message` envelope,
  see [05-bus.md](modules/05-bus.md) §9)
- [x] Basic `internal/orchestrator`: pipeline step linking

**Documents:** [01-scheduler.md](modules/01-scheduler.md), [02-collector.md](modules/02-collector.md),
[03-parser.md](modules/03-parser.md), [04-deduplicator.md](modules/04-deduplicator.md),
[05-bus.md](modules/05-bus.md), [06-orchestrator.md](modules/06-orchestrator.md)

**Output:** monolithic `gruh` (root command) populates DB with new GitHub Atom feed updates.

## Phase 3 — LLM Classification (completed)

**Goal:** separating important updates from noise.

- [x] `internal/llm`: OpenAI-compatible client (timeouts, retry, token accounting)
- [x] `internal/prompt`: built-in prompts via `go:embed`, override from user directory, Go templates;
  YAML prompt header (`name`/`version`/`critical`/`description`, see [09-prompt.md](modules/09-prompt.md) §4)
- [x] `internal/classificator`: importance verdict; context = current update + 2 last important;
  confidence threshold 0.5, security patches always important (rule on top of LLM)
- [x] Storing verdicts and history of important updates in `storage`
- [x] LLM unavailability (after retries) — fail fast: error and crash without saving
  classification state (fallback between models — on LiteLLM side, not in app)
- [x] LLM telemetry: classification traces in **Langfuse** via OTEL/OTLP (GenAI attributes, tokens, prompt version)

**Documents:** [07-classificator.md](modules/07-classificator.md), [08-llm.md](modules/08-llm.md),
[09-prompt.md](modules/09-prompt.md), [12-observability.md](modules/12-observability.md)

**Output:** each new update receives an important/noise verdict with explanation.

## Phase 4 — Notification Delivery

**Goal:** users receive notifications about important updates.

- [x] `internal/dispatcher`: general `Notifier` interface
- [x] Implementations: Webhook → Slack → Telegram
- [x] Notification text templates: Go template, defaults via `go:embed`,
- [x] Routing based on `Feed URL -> channels` mapping
- [x] Delivery retry policy and protection against duplicate sending

**Documents:** [10-dispatcher.md](modules/10-dispatcher.md)

**Output:** full MVP cycle: feed → classification → notification to channel.

## Phase 5 — Distributed Mode (Redis)

**Goal:** horizontal scaling in k8s.

- [x] Redis-based `internal/bus` implementation (Streams + consumer groups, ack/retry, DLQ)
- [x] Process role separation: collector / worker(classificator) / dispatcher —
  subcommands `gruh collector | worker | dispatcher` appear here (root command without subcommand = monolith)
- [x] Idempotency guarantees during message re-delivery from bus
- [x] Distributed scheduler locks (multiple scheduler replicas)

**Documents:** [05-bus.md](modules/05-bus.md), [06-orchestrator.md](modules/06-orchestrator.md)

## Phase 6 — Deployment and Operations

**Goal:** production-ready deployment.

- [x] `Dockerfile` (multi-stage, distroless)
- [ ] Helm charts in `deploy/`, `skaffold.yaml`
- [ ] HPA based on queue/CPU metrics
- [x] Prometheus metrics for all modules (`/metrics`), health/readiness probes
- REJECTED: end-to-end pipeline tracing — OTEL tracing remains only for LLM
  and only for Langfuse (decision made, see [12-observability.md](modules/12-observability.md) §9)

**Documents:** [12-observability.md](modules/12-observability.md)

## Phase 7 — Future Development (backlog)

- [ ] Control transports for feeds and channels — Slack/Telegram bot (add/list/delete
  feeds directly from messenger); optionally — Web UI / API
- [ ] Digests (aggregating multiple updates into one notification): disabled
  by default, enabled and formed separately for each channel
  (schedule and per-channel template, see [10-dispatcher.md](modules/10-dispatcher.md) §4)
- [ ] Retention job for `raw_contents` (cleaning up old raw content,
  see [11-storage.md](modules/11-storage.md) §9)
- [ ] Additional source types (non-GitHub RSS, changelog pages)
- [ ] Classification quality evaluation (feedback loop, flagging false positives)
- [ ] LLM call caching/budgeting

## Phase Dependencies

```
Phase 0 ──▶ Phase 1 ──▶ Phase 2 ──▶ Phase 3 ──▶ Phase 4 ──▶ Phase 5 ──▶ Phase 6 ──▶ Phase 7
                        (MVP-core)  (intelligence)  (MVP)     (scaling)  (prod)
```

Phases 5 and 6 can be executed in parallel after completing Phase 4.
