# gruh — Go RSS Update Handler

`gruh` polls RSS/Atom feeds (primarily GitHub `releases.atom`), uses an LLM to
separate **noise** (dev tags, RC/pre-releases, minor bumps) from **important**
updates (major releases, breaking changes, critical security fixes), and
notifies you only about what matters — via Slack, Telegram, or webhooks.

It ships as a single static binary with an embedded read-only web UI and
Prometheus metrics. Storage is SQLite (zero-dependency, pure Go) or PostgreSQL.

## What it does

- **Pulls** each active feed on a schedule (with jitter), honoring `ETag` /
  `If-Modified-Since` so unchanged feeds cost nothing.
- **Parses** RSS/Atom into a unified event and **deduplicates** by fingerprint,
  so each update is processed once.
- **Classifies** every new update with an LLM, using the two most recent
  *important* updates of the same feed as context. The verdict carries
  `important`, `category`, `confidence`, `reason`, and a short generated title.
- **Dispatches** important updates to the channels mapped to their feed, exactly
  once per channel.
- **Resumes on restart**: updates left unclassified or undelivered are re-driven
  on startup and on every poll tick, so nothing is silently stranded.

## How it works

```
┌───────────┐   ┌───────────┐   ┌────────┐   ┌──────────────┐
│ Scheduler │──▶│ Collector │──▶│ Parser │──▶│ Deduplicator │
└───────────┘   └───────────┘   └────────┘   └──────┬───────┘
                                                     │ new update
                                                     ▼
                                                 ┌───────┐
                                                 │  Bus  │  (in-process; Redis planned)
                                                 └───┬───┘
                              updates.new  ┌─────────┴─────────┐  updates.important
                                           ▼                   ▼
                                   ┌───────────────┐    ┌────────────┐
                                   │ Classificator │    │ Dispatcher │──▶ Slack / Telegram / Webhook
                                   │   (LLM)       │    └────────────┘
                                   └───────┬───────┘
                                           │ verdict.important ⇒ publish to updates.important
                                           ▼
                                     ┌──────────┐
                                     │ Storage  │  feeds · updates · verdicts · dispatches · channels
                                     └──────────┘
```

1. **Scheduler** fires the collection task at `scheduler.interval` (plus jitter);
   the first run happens immediately at startup.
2. **Collector** fetches the feed (conditional GET, retries, per-host rate limit).
3. **Parser** normalizes RSS/Atom/JSON into an internal event.
4. **Deduplicator** drops updates already stored (unique fingerprint).
5. New updates are persisted and published to the `updates.new` topic.
6. The **classification worker** consumes `updates.new`, calls the LLM, saves the
   verdict, and — if important — publishes to `updates.important`.
7. The **dispatcher** consumes `updates.important` and delivers to each mapped
   channel, guarded by a `dispatches` table so no channel is notified twice.

> The bus is currently in-memory (single process). Because it does not persist or
> redeliver, `ReconcilePending` re-queries the database for unclassified and
> undelivered updates on every tick and republishes them — this is what makes
> restarts safe. A Redis-backed bus (enabling the split roles below to run as
> separate processes) is planned.

## Getting started

Requires **Go 1.26.5+**.

```sh
make build          # -> bin/gruh
cp config.example.yaml config.yaml   # then edit
./bin/gruh --config config.yaml --check-config   # validate config
./bin/gruh --config config.yaml                  # run
```

Feeds live in the `feeds` table. For local use, seed some:

```sh
sqlite3 data/gruh.db < data/seed.sql
```

## Configuration

Config is a YAML file (default path `config.yaml`, override with `--config`).
Secrets and common knobs can also come from environment variables, which take
precedence.

```yaml
storage:
  driver: sqlite               # "sqlite" or "postgres"
  dsn: data/gruh.db            # sqlite file, or a postgres DSN
  raw_content_retention: 90d   # cleanup period for raw_contents; 0/empty = keep forever

llm:
  base_url: https://your-endpoint/v1   # OpenAI-compatible API
  model: qwen3-32b
  # tls: { insecure: true }    # only for self-signed/local endpoints

scheduler:
  interval: 5m                 # poll active feeds every 5m
  jitter: 30s

dispatcher:
  slack:
    releases: https://hooks.slack.com/services/XXX/YYY/ZZZ   # name -> webhook URL
  telegram:
    ops: { token: "123:ABC", chat_id: "-1001234567890" }     # name -> params

observability:
  log: { level: info, format: json }
  metrics: ":9090"             # /metrics, /healthz, /readyz ("" disables)

webui:
  addr: ":8080"                # read-only updates UI ("" disables)
```

Environment overrides (secrets should use these):

| Variable | Maps to |
|---|---|
| `GRUH_DB_DSN` | `storage.dsn` |
| `GRUH_STORAGE_DRIVER` | `storage.driver` |
| `GRUH_LLM_BASE_URL` / `GRUH_LLM_MODEL` / `GRUH_LLM_API_KEY` | `llm.*` |
| `GRUH_LLM_TIMEOUT` / `GRUH_LLM_RETRIES` / `GRUH_LLM_CONCURRENT` / `GRUH_LLM_TEMP` | `llm.*` |
| `GRUH_SCHEDULER_INTERVAL` / `GRUH_SCHEDULER_JITTER` | `scheduler.*` |
| `GRUH_COLLECTOR_TIMEOUT` / `GRUH_COLLECTOR_RETRIES` | `collector.*` |
| `GRUH_LOG_LEVEL` / `GRUH_LOG_FORMAT` | `observability.log.*` |

The database schema is created and reconciled automatically (GORM `AutoMigrate`)
on startup — no manual migration step.

## Commands

```
gruh [--config PATH] [--check-config]   # run the full pipeline in one process (default)
gruh collector                          # collection + reconcile only
gruh worker                             # classification worker only
gruh dispatcher                         # delivery only
```

The default (root) command runs everything in one process and is the intended
mode today. The split roles share the same code paths but require the planned
Redis bus to communicate across processes.

## Docker

Multi-arch images are published to GHCR by CI on pushes to `main` and `v*` tags:

```sh
docker run --rm \
  -v "$PWD/config.yaml:/app/config.yaml:ro" \
  -v "$PWD/data:/app/data" \
  -p 8080:8080 -p 9090:9090 \
  ghcr.io/jetmaniack/go-rss-update-handler:latest
```

The image is a static (`CGO_ENABLED=0`) binary on Alpine, running as a non-root
user. Mount a writable `data/` volume when using the SQLite driver.

## Observability & UI

- **Web UI** — `http://localhost:8080`: read-only, paginated table of updates
  with category/importance filters and the raw release text.
- **Metrics** — `http://localhost:9090/metrics` (Prometheus), plus `/healthz`
  and `/readyz`.

## Development

```sh
make test           # go test ./...
make lint           # golangci-lint (installed with the module's Go toolchain)
make gosec          # gosec security scan
make govulncheck    # known-vulnerability scan
make security       # gosec + govulncheck + staticcheck
```

CI (`.github/workflows/ci.yml`) runs vet, tests, lint, gosec, govulncheck and a
Docker build on every pull request; `publish.yml` builds and pushes the image
after tests pass on `main`/tags.
