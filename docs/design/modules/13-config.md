# 13. Config (`internal/config`)

## 1. Purpose

A cross-cutting module for loading, merging, and validating the application configuration.
The single point where the config file and environment variables are read;
other modules receive already-prepared typed structs via DI
and know nothing about the configuration sources.

## 2. Responsibilities and Boundaries

**Does:**
* Loads `config.yaml` (path — `--config` flag / `GRUH_CONFIG` env, default `./config.yaml`).
* Overlays environment variables on top of the file (see priorities in §4).
* Defaults for all optional parameters.
* Validates the entire configuration at startup — fail fast with a human-readable list of errors.
* Provides typed sections (`config.Scheduler`, `config.LLM`, …) to modules.

**Does NOT:**
* Does not store domain data: **feeds, channels, and their mapping live in the DB** (source of truth —
  `storage`; currently managed directly in the DB via seed/SQL scripts; management transports —
  a Slack/Telegram bot — will be developed as a separate step, phase 7); they are not in the config.
* Does not re-read config at runtime (no hot-reload — changes are applied by restarting;
  exception — user prompts, that is the `internal/prompt` zone).
* Does not read secrets from a file (see §4 "Secrets").

## 3. Public Interface

```go
// Load reads the file, overlays env, applies defaults, and validates.
func Load(path string) (*Config, error)

type Config struct {
	Scheduler     scheduler.Config
	Collector     collector.Config
	Parser        parser.Config
	Deduplicator  deduplicator.Config
	Bus           bus.Config
	Orchestrator  orchestrator.Config
	Classificator classificator.Config
	LLM           llm.Config
	Prompt        prompt.Config
	Dispatcher    dispatcher.Config
	Storage       storage.Config
	Observability observability.Config
}

// Validate returns ALL errors at once (errors.Join), not just the first one.
func (c *Config) Validate() error
```

Each module declares its own `Config` type in its own package; `internal/config`
only aggregates them — this way the module's configuration lives alongside its code and tests.

## 4. Internal Design

### Sources and Priorities (descending)

1. **Environment variables** — `GRUH_` + key path via `_` in upper case:
   `llm.base_url` → `GRUH_LLM_BASE_URL`.
2. **File** `config.yaml`.
3. **Defaults** in code.

### Library

`github.com/knadh/koanf/v2` (providers `file` + `env`, parser `yaml`):
a lightweight alternative to viper without global state and unnecessary dependencies,
natively supports source merging and unmarshaling into structs.
Validation is done manually in `Validate()` of each section (no heavy validator libraries).

### Secrets

Secrets are set **only** via env and are not stored in the file (validation raises an error
if a secret appears in YAML):

| Secret | Variable |
|--------|------------|
| Database DSN | `GRUH_DB_DSN` |
| LLM API key | `GRUH_LLM_API_KEY` |
| Langfuse keys | `GRUH_LANGFUSE_PUBLIC_KEY` / `GRUH_LANGFUSE_SECRET_KEY` |
| Channel tokens (Slack/Telegram) | env only: in `channels.config_json` (DB) only the **name** of the variable is stored (`token_env`), see §9 |

### "Config vs DB" Separation (accepted decision)

| Data | Lives in |
|--------|-----------|
| Feeds (URL, active flag) | **DB** (source of truth) |
| Notification channels and feed → channel mapping | **DB** (source of truth) |
| Polling intervals, jitter, limits | **config** (global values) |
| Technical module parameters (timeouts, retries, ports) | **config** |

### Example `config.example.yaml`

File at the repository root, aggregates sections from all modules (see §6 of the respective docs):

```yaml
scheduler:
  poll_interval: 15m
  jitter: 20%
storage:
  driver: postgres
llm:
  base_url: http://vllm.internal:8000/v1
  model: qwen3-32b
observability:
  log: { level: info, format: json }
# secrets: GRUH_DB_DSN, GRUH_LLM_API_KEY, GRUH_LANGFUSE_*
```

## 5. Dependencies

* `github.com/knadh/koanf/v2` (+ `file`, `env`, `yaml` providers).
* `Config` types of all `internal/*` modules.

## 6. Configuration

The module itself is configured only by a flag/variable with the file path:
`--config <path>` / `GRUH_CONFIG` (default `./config.yaml`; a missing file is not an error
if everything required is provided via env/defaults). `--config` is a flag of the single
root command `gruh` (see [00-overview.md](../00-overview.md) §7).

The **`--check-config`** flag (accepted, phase 0): load and validate the configuration
without starting the service: exit code 0 and a brief report on success, otherwise — list of
errors and a non-zero code. Useful in CI and k8s (init check before deploy).

## 7. Errors and Edge Cases

* Any load/validation error — **fail fast**: the process does not start, all errors
  are printed as a list (`errors.Join`), not one by one.
* Unknown keys in YAML — error (protection against typos), strict unmarshal.
* Secret in YAML file — validation error with a hint to "use env".
* Mutually exclusive combinations (e.g., `storage.driver: sqlite` + distributed mode) —
  checked in `Validate()`.

## 8. Testing

* Unit: priority env > file > default; unmarshal of all sections from `config.example.yaml`
  (the example file is always valid — golden test).
* Unit: `Validate()` — one test per rule (unknown key, secret in file,
  invalid combinations).

## 9. Open Questions and Accepted Decisions

* **Channel token encryption — dropped (no secrets in DB)**: channels do indeed
  live in the DB (`channels`), but `config_json` stores not the token itself but the **name
  of the env variable** (`"token_env": "GRUH_TG_TOKEN"`, see [10-dispatcher.md](10-dispatcher.md) §6);
  the value is read from the environment (k8s Secret) at startup. Nothing to encrypt in the DB —
  the "secrets via env only" policy applies uniformly to both the config file and the DB.
  Revisit if management transports (phase 7) require adding channels with tokens on the fly,
  without redeploying env.
* **`gruh --check-config` flag — resolved (yes, phase 0)**: config validation without starting
  the service (see §6) — the validator is planned anyway, the flag is cheap. No separate command —
  the CLI consists of a single root command (see [00-overview.md](../00-overview.md) §7).
