# GRUH — Design Documentation

Hierarchy of design documents for the **GRUH (Go RSS Update Handler)** project — an AI-powered RSS/Atom feed processing system.
A high-level architectural description is in [00-overview.md](00-overview.md).

## Documentation Structure

```
docs/design/
├── README.md          # This file — documentation index
├── 00-overview.md     # General architectural overview, data flows, cross-cutting decisions
├── ROADMAP.md         # Development roadmap by phases
└── modules/           # Design documents for each internal/ module
    ├── 01-scheduler.md
    ├── 02-collector.md
    ├── 03-parser.md
    ├── 04-deduplicator.md
    ├── 05-bus.md
    ├── 06-orchestrator.md
    ├── 07-classificator.md
    ├── 08-llm.md
    ├── 09-prompt.md
    ├── 10-dispatcher.md
    ├── 11-storage.md
    ├── 12-observability.md
    └── 13-config.md
```

Module numbering corresponds to the order in which an event passes through the pipeline
(scheduler → collector → parser → deduplicator → bus → orchestrator → classificator/llm/prompt → dispatcher);
the `storage`, `observability`, and `config` modules are cross-cutting and are described last.

## Documents

| # | Document | Module | Purpose |
|---|----------|--------|------------|
| — | [00-overview.md](00-overview.md) | — | Overall architecture, pipeline, data model, stack |
| 1 | [01-scheduler.md](modules/01-scheduler.md) | `internal/scheduler` | Feed polling task scheduling with jitter |
| 2 | [02-collector.md](modules/02-collector.md) | `internal/collector` | HTTP feed fetching, rate limiting, ETag |
| 3 | [03-parser.md](modules/03-parser.md) | `internal/parser` | Parsing RSS/Atom/JSON into a unified format |
| 4 | [04-deduplicator.md](modules/04-deduplicator.md) | `internal/deduplicator` | Fingerprinting and uniqueness checking |
| 5 | [05-bus.md](modules/05-bus.md) | `internal/bus` | Unified event bus (Redis) |
| 6 | [06-orchestrator.md](modules/06-orchestrator.md) | `internal/orchestrator` | Event processing flow management |
| 7 | [07-classificator.md](modules/07-classificator.md) | `internal/classificator` | LLM-based update importance classification |
| 8 | [08-llm.md](modules/08-llm.md) | `internal/llm` | OpenAI-compatible client |
| 9 | [09-prompt.md](modules/09-prompt.md) | `internal/prompt` | Prompt management (.md + Go templates) |
| 10 | [10-dispatcher.md](modules/10-dispatcher.md) | `internal/dispatcher` | Notification delivery (Slack, Telegram, Webhook) |
| 11 | [11-storage.md](modules/11-storage.md) | `internal/storage` | Repository layer (GORM: PostgreSQL/SQLite) |
| 12 | [12-observability.md](modules/12-observability.md) | `internal/observability` | Logging (slog), metrics (Prometheus), LLM telemetry (Langfuse + OTEL) |
| 13 | [13-config.md](modules/13-config.md) | `internal/config` | Configuration loading and validation (YAML + env) |
| — | [ROADMAP.md](ROADMAP.md) | — | Development phases and implementation order |

## Development Process

The project is developed according to the following rules (mandatory for all phases and modules):

* **TDD (Test-Driven Development)** — a failing test is written first, then a minimal
  implementation, then refactoring (red → green → refactor cycle). Code without tests is not merged.
* **GitFlow (feature branches)** — a separate branch is created for each feature
  (`feature/<short-name>`); after all tests pass and the review is complete, the branch is merged into `main`.
  The `main` branch always remains in a working (green) state.

For more details, see the "Development Process" section in [ROADMAP.md](ROADMAP.md).

## Document Conventions

Each module design document follows a uniform template:

1. **Purpose** — why the module is needed and its place in the pipeline.
2. **Responsibilities and Boundaries** — what the module does and what it explicitly does NOT do.
3. **Public Interface** — expected Go interfaces and types.
4. **Internal Design** — key decisions and algorithms.
5. **Dependencies** — which modules/libraries it depends on.
6. **Configuration** — parameters from `config.yaml`.
7. **Error Handling and Edge Cases**.
8. **Testing** — the module's testing strategy.
9. **Open Questions** — what needs clarification before implementation.
