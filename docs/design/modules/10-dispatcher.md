# 10. Dispatcher (`internal/dispatcher`)

## 1. Purpose

The final stage of the pipeline: delivers notifications about important updates to the configured channels.
A single generic interface covers different transports: **Slack, Telegram, Webhook** (extensible).

## 2. Responsibilities and Boundaries

**Does:**
* Resolves the channel list by the `Feed URL -> [channels]` mapping.
* Formats the notification for a specific channel (Slack blocks / Telegram markdown / JSON webhook).
* Delivers with retries; reports delivery status per channel.

**Does NOT:**
* Does not decide what is important (that is `classificator`).
* Does not store the feed-to-channel mapping (that is `storage`/config).
* Does not handle protection against repeated event sending at the event level (that is orchestrator + storage).

## 3. Public Interface

```go
// Notifier — the unified notification transport interface.
type Notifier interface {
	// Name — unique channel identifier (e.g., "slack_channel_1").
	Name() string
	Send(ctx context.Context, n Notification) error
}

type Notification struct {
	Event   UpdateEvent
	Verdict classificator.Verdict
	FeedURL string
}

// Dispatcher routes a notification to all channels of the feed.
type Dispatcher interface {
	// Dispatch returns per-channel results; partial failure does not cancel other deliveries.
	Dispatch(ctx context.Context, n Notification, channels []string) (Report, error)
}

type Report map[string]error // channel name -> nil | error
```

## 4. Internal Design

### Notifier Implementations

| Transport | Mechanism | Format |
|-----------|----------|--------|
| `webhook` | HTTP POST JSON to URL | `{"feed_url", "source_url", "title", "category", "reason", "published_at"}` |
| `slack` | Incoming Webhook / chat.postMessage | Block Kit: header, category, LLM explanation, link |
| `telegram` | Bot API `sendMessage` | MarkdownV2, special character escaping |

* Channels are declared in the config with type and parameters; a factory creates a `Notifier` by type.
* Channels for one event are notified concurrently (`errgroup`); failure of one does not block others.
* Per-channel retries: backoff on 429/5xx/network; final failure goes into `Report` and the log.
* Channel secrets (tokens, webhook URLs) — from env variables; only references to them in the config.

### Notification Text Templates (accepted decision)

* The notification text is rendered via **`text/template` (Go template)**; template data is
  `Notification` (event, verdict, feed URL).
* Default per-transport templates are embedded in the binary (`go:embed`), as in `internal/prompt`.
* Override — a separate template file, the path is specified in the config
  (`dispatcher.templates.<transport>`); the specified template completely replaces the default.
* Broken template (parse error) — validation error at startup (fail fast).

### Digest Mode (accepted decision)

* Digests are implemented at the dispatcher level, but **disabled by default** —
  enabled separately through config, **individually for each channel**.
* A channel with `digest.enabled: true` does not receive instant notifications: important events
  are buffered (persistently — by the facts in `dispatches` for not-yet-delivered events)
  and sent as a single message on the channel's schedule (`digest.schedule`, cron format).
* Digest formation is per-channel: each channel aggregates only events from its own feeds
  (by the `feed_channels` mapping) and renders its own digest template
  (`dispatcher.templates.<transport>_digest`, also a Go template with override support).
* Mixed mode is supported: some channels receive instant notifications, others receive digests.

## 5. Dependencies

* stdlib `net/http` (all transports are regular HTTP APIs, heavy SDKs are not required).
* stdlib `text/template` — rendering of notification and digest text (see §4).
* `internal/classificator` — type `Verdict` (or shared `internal/model`).

## 6. Configuration

```yaml
dispatcher:
  templates:                       # override built-in templates (Go template);
    slack: ./templates/slack.tmpl  # if not specified — use default from go:embed
    telegram_digest: ./templates/tg_digest.tmpl
```

Channels and the feed mapping live in the **DB** (`channels`, `feed_channels` — see
[11-storage.md](11-storage.md)); parameters of a specific channel, including digest settings,
are in `channels.config_json`:

```json
{
  "type": "telegram",
  "token_env": "GRUH_TG_TOKEN",
  "chat_id": "-1001234567",
  "digest": { "enabled": true, "schedule": "0 10 * * *" }
}
```

## 7. Errors and Edge Cases

* Channel in the mapping is not declared in `channels` — config validation error at startup.
* Partial failure (1 of N channels) — successful channels are marked as delivered, only the failed one is retried.
* Slack/Telegram rate limits — respect `Retry-After`, per-channel rate limiter.
* Message too long — truncated to transport limit (Slack 3000, Telegram 4096) with a link to the source.
* Empty channel list for a feed — no-op with warning (classified, but no one to notify).

## 8. Testing

* Unit per-transport with `httptest.Server`: payload format, retries, 429 handling.
* Unit Dispatcher: concurrent delivery, partial failures, `Report`.

## 9. Open Questions and Accepted Decisions

* **Notification templates — resolved**: Go template (`text/template`), defaults embedded
  via `go:embed`, override — a separate file with path in config (`dispatcher.templates.*`),
  which replaces the default (see §4).
* **Digests — resolved**: implemented at the dispatcher level (phase 7), disabled
  by default, enabled individually per channel; formation is per-channel
  with its own schedule and template (see §4).
