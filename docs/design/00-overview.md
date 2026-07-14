# 00. GRUH Architectural Overview

## 1. System Purpose

**GRUH (Go RSS Update Handler)** is a system for processing RSS/Atom feeds (primarily GitHub Atom)
that uses an LLM to separate "noise" (dev tags, RC releases, minor bumps) from "important" updates
(major releases, breaking changes, critical security patches) and notifies users only about what matters.

## 2. Operational Model: Pull + Pipeline

The system is built on a **pull model**: the scheduler initiates a polling task at each feed's individual interval.
**Jitter** is applied to smooth peak load and respect source rate limits.

An event passes through a pipeline of modules connected by a shared bus:

```
┌───────────┐   ┌───────────┐   ┌────────┐   ┌──────────────┐
│ Scheduler │──▶│ Collector │──▶│ Parser │──▶│ Deduplicator │
└───────────┘   └───────────┘   └────────┘   └──────┬───────┘
                                                    │ (new event)
                                                    ▼
                                              ┌─────────┐
                                              │   Bus   │ (Redis)
                                              └────┬────┘
                                                   ▼
                                          ┌──────────────┐
                                          │ Orchestrator │
                                          └──────┬───────┘
                                                 ▼
                    ┌───────────────┐   ┌────────────────┐
                    │ Prompt ◀─ LLM │◀──│ Classificator  │
                    └───────────────┘   └───────┬────────┘
                                                │ (important == true)
                                                ▼
                                         ┌────────────┐
                                         │ Dispatcher │──▶ Slack / Telegram / Webhook
                                         └────────────┘

          ┌─────────┐
          │ Storage │ ◀── cross-cutting layer (feeds, updates, important update history, channels)
          └─────────┘
```

Processing steps:

1. **Scheduler** — triggers a task at the feed's interval (with jitter).
2. **Collector** — downloads raw data (mandatory `If-Modified-Since` / `ETag`).
3. **Parser** — converts RSS/Atom/JSON into a unified internal format.
4. **Deduplicator** — discards already-known updates by fingerprint.
5. **Bus** — carries the event through the pipeline; enrichment is passed via `context.Context`.
6. **Orchestrator** — manages the flow, guaranteeing that the event passes through the required modules.
7. **Classificator (LLM)** — analyzes the update + the context of the **two most recent important updates**.
8. **Dispatcher** — when the verdict is "important", sends notifications to the configured channels.

## 3. Repository Structure

```
cmd/gruh/            # Entry point (urfave/cli v3, single root command, see §7)
deploy/              # Helm charts
docs/
├── design/          # Design documents (this directory)
└── features/        # Feature descriptions
internal/
├── bus/             # Unified event bus (Redis)
├── classificator/   # LLM-based decision logic
├── collector/       # HTTP fetching and rate limiting
├── config/          # Configuration loading and validation (YAML + env, koanf)
├── deduplicator/    # Fingerprinting and uniqueness checking
├── dispatcher/      # Notification delivery (Slack, Telegram, Webhook)
├── llm/             # OpenAI-compatible client
├── model/           # Shared data types (UpdateEvent), no dependencies
├── observability/   # Logging (slog), metrics (Prometheus), LLM telemetry (Langfuse + OTEL)
├── orchestrator/    # Orchestration of the classification process
├── parser/          # RSS/Atom/JSON parsing (gofeed)
├── prompt/          # Prompt management (.md + Go templates, go:embed)
├── scheduler/       # Task scheduling with jitter
└── storage/         # Repository layer (GORM: PostgreSQL/SQLite)
Dockerfile
config.example.yaml
Makefile
README.md
skaffold.yaml
```

## 4. Data Model

The core of the system is the `UpdateEvent` (defined in the `internal/model` package — an accepted
decision, see [03-parser.md](modules/03-parser.md) §9), enriched as it moves through the pipeline
via `context.Context`:

```go
type UpdateEvent struct {
	SourceURL   string
	RawContent  string
	PublishedAt time.Time
	Fingerprint string
} // Enrichment is added to context.Context as the event moves through the bus.
```

### Feed-to-Notification-Channel Mapping

Instead of per-user configs, a direct mapping is used:

```
Feed URL -> list of notification channels
{"https://github.com/user/repo/releases.atom": ["slack_channel_1", "webhook_url_x"]}
```

**Source of truth (accepted decision):** feeds, channels, and their mapping are stored in the **DB**
(currently managed directly in the DB via seed/SQL scripts; management transports — a Slack/Telegram bot —
will be developed as a separate step, see the roadmap, phase 7), while polling intervals
and technical parameters live in the **config**. See [13-config.md](modules/13-config.md).

## 5. Technology Stack

| Area | Solution |
|---------|---------|
| Language | Go 1.26 |
| CLI | `urfave/cli/v3` |
| DB/ORM | GORM (PostgreSQL — production, SQLite — local/tests) |
| Configuration | `koanf/v2` (YAML + env, priority env > file > defaults) |
| Parsing | `gofeed` |
| LLM | OpenAI-compatible API client |
| Broker | Redis (distributed processing, scaling in k8s) |
| Logging | `log/slog` (structured logs, text/JSON) |
| Metrics | Prometheus (`prometheus/client_golang`, endpoint `/metrics`) |
| LLM telemetry | Langfuse + OpenTelemetry (OTLP, GenAI semantic conventions) |
| CI | GitHub Actions (`.github/workflows`) |
| Deployment | Kubernetes + HPA, Helm, Skaffold |
| Local development | docker-compose (PostgreSQL, Redis) |

## 6. Key Cross-Cutting Decisions and Constraints

* **LLM context:** the classificator receives the current update **and the data from the two most recent
  important updates** of that feed in order to assess the scope of changes by comparison. The important
  update history is stored by `storage`.
* **Prompts:** managed via `.md` files with Go templates. Files in the user directory override
  the built-in ones (`go:embed`).
* **GitHub:** optimization via `ETag` / `Last-Modified` is mandatory.
* **Scalability:** the Redis bus allows decoupling `collector` from `classificator` and scaling
  components independently (multiple pods in k8s with HPA).
* **Idempotency:** deduplication by fingerprint guarantees that a single update will not be
  classified and dispatched twice, even with repeated delivery from the bus.
* **Observability:** structured logs (`log/slog`) with event context, Prometheus metrics across all
  pipeline modules, LLM call telemetry via Langfuse + OTEL
  (prompts, tokens, verdicts). See [12-observability.md](modules/12-observability.md).
* **Configuration:** YAML + env (env has priority), secrets via env only, fail-fast
  validation on startup. Feeds/channels — in the DB, intervals/technical parameters — in the config.
  See [13-config.md](modules/13-config.md).

## 7. Run Modes and CLI (accepted decision)

* **Monolith** — all modules in a single process, in-memory bus (or Redis) — for simple installations and local development.
* **Distributed** — `collector` / `worker (classificator)` / `dispatcher` roles in separate pods, communicating via Redis.

The target environment is **Kubernetes**: manual command execution is unnecessary overhead, so
the application is designed as **zero-touch** — the pod simply starts and runs
(migrations, initialization — automatically on startup). The CLI design (`cmd/gruh`,
`urfave/cli/v3`) is **minimalist**:

* **Single root command** `gruh` — starts the full pipeline (monolith); there are no separate
  `serve` / `migrate` / `version` commands: version is a `--version` flag (built into `urfave/cli`),
  migrations run automatically on startup (fail fast on error).
* **Root command flags:** `--config <path>` (path to YAML, env `GRUH_CONFIG`), `--version`,
  `--check-config` (validate config without starting the service, see [13-config.md](modules/13-config.md) §6).
* **Subcommands are reserved only for microservice roles** (phase 5, distributed mode):
  `gruh collector`, `gruh worker`, `gruh dispatcher` — start the process in the corresponding role;
  the root command without a subcommand remains the monolith. No other subcommands are introduced.
