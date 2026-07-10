# 10. Dispatcher (`internal/dispatcher`)

## 1. Назначение

Финальная стадия пайплайна: доставляет уведомления о важных обновлениях в настроенные каналы.
Единый generic-интерфейс покрывает разные транспорты: **Slack, Telegram, Webhook** (расширяемо).

## 2. Ответственность и границы

**Делает:**
* Резолвит список каналов по маппингу `Feed URL -> [каналы]`.
* Форматирует уведомление под конкретный канал (Slack blocks / Telegram markdown / JSON webhook).
* Доставляет с ретраями; репортит статус доставки по каждому каналу.

**НЕ делает:**
* Не решает, что важно (это `classificator`).
* Не хранит маппинг фидов на каналы (это `storage`/конфиг).
* Не занимается защитой от повторной отправки на уровне событий (это orchestrator + storage).

## 3. Публичный интерфейс

```go
// Notifier — единый интерфейс транспорта уведомления.
type Notifier interface {
	// Name — уникальный идентификатор канала (например, "slack_channel_1").
	Name() string
	Send(ctx context.Context, n Notification) error
}

type Notification struct {
	Event   UpdateEvent
	Verdict classificator.Verdict
	FeedURL string
}

// Dispatcher маршрутизирует уведомление во все каналы фида.
type Dispatcher interface {
	// Dispatch возвращает per-channel результаты; частичный сбой не отменяет остальные доставки.
	Dispatch(ctx context.Context, n Notification, channels []string) (Report, error)
}

type Report map[string]error // имя канала -> nil | ошибка
```

## 4. Внутреннее устройство

### Реализации Notifier

| Транспорт | Механизм | Формат |
|-----------|----------|--------|
| `webhook` | HTTP POST JSON на URL | `{"feed_url", "source_url", "title", "category", "reason", "published_at"}` |
| `slack` | Incoming Webhook / chat.postMessage | Block Kit: заголовок, категория, объяснение LLM, ссылка |
| `telegram` | Bot API `sendMessage` | MarkdownV2, экранирование спецсимволов |

* Каналы объявляются в конфиге с типом и параметрами; фабрика создаёт `Notifier` по типу.
* Каналы одного события уведомляются конкурентно (`errgroup`), сбой одного не блокирует другие.
* Ретраи per-channel: backoff на 429/5xx/сетевые; финальный сбой попадает в `Report` и в лог.
* Секреты каналов (токены, webhook URL) — из env-переменных, в конфиге только ссылки на них.

### Шаблоны текста уведомлений (принятое решение)

* Текст уведомления рендерится через **`text/template` (Go template)**; данные шаблона —
  `Notification` (событие, вердикт, URL фида).
* Дефолтные шаблоны per-transport встроены в бинарь (`go:embed`), как в `internal/prompt`.
* Оверрайд — отдельный файл шаблона, путь указывается в конфиге
  (`dispatcher.templates.<transport>`); заданный шаблон полностью заменяет дефолт.
* Битый шаблон (ошибка парсинга) — ошибка валидации на старте (fail fast).

### Дайджест-режим (принятое решение)

* Дайджесты реализуются на уровне dispatcher'а, но **выключены по умолчанию** —
  включаются отдельно через конфиг, **индивидуально для каждого канала**.
* Канал с `digest.enabled: true` не получает мгновенных уведомлений: важные события
  буферизуются (персистентно — по фактам в `dispatches` ещё не доставленных событий)
  и отправляются одним сообщением по расписанию канала (`digest.schedule`, cron-формат).
* Формирование дайджеста — per-channel: каждый канал агрегирует только события своих
  фидов (по маппингу `feed_channels`) и рендерит собственный digest-шаблон
  (`dispatcher.templates.<transport>_digest`, тоже Go template с оверрайдом).
* Смешанный режим допустим: одни каналы получают мгновенные уведомления, другие — дайджесты.

## 5. Зависимости

* stdlib `net/http` (все транспорты — обычные HTTP API, тяжёлые SDK не требуются).
* stdlib `text/template` — рендеринг текста уведомлений и дайджестов (см. §4).
* `internal/classificator` — тип `Verdict` (или общий `internal/model`).

## 6. Конфигурация

```yaml
dispatcher:
  templates:                       # оверрайд встроенных шаблонов (Go template);
    slack: ./templates/slack.tmpl  # не указан — используется дефолт из go:embed
    telegram_digest: ./templates/tg_digest.tmpl
```

Каналы и маппинг фидов живут в **БД** (`channels`, `feed_channels` — см.
[11-storage.md](11-storage.md)); параметры конкретного канала, включая дайджест-настройки,
— в `channels.config_json`:

```json
{
  "type": "telegram",
  "token_env": "GRUH_TG_TOKEN",
  "chat_id": "-1001234567",
  "digest": { "enabled": true, "schedule": "0 10 * * *" }
}
```

## 7. Ошибки и крайние случаи

* Канал в маппинге не объявлен в `channels` — ошибка валидации конфига на старте.
* Частичный сбой (1 из N каналов) — успешные каналы помечаются доставленными, ретраится только упавший.
* Rate limits Slack/Telegram — уважение `Retry-After`, per-channel rate limiter.
* Слишком длинное сообщение — усечение под лимит транспорта (Slack 3000, Telegram 4096) со ссылкой на источник.
* Пустой список каналов у фида — no-op c warning (классифицировали, но некому сообщить).

## 8. Тестирование

* Unit per-transport с `httptest.Server`: формат payload, ретраи, обработка 429.
* Unit Dispatcher: конкурентная доставка, частичные сбои, отчёт `Report`.

## 9. Открытые вопросы и принятые решения

* **Шаблоны уведомлений — решено**: Go template (`text/template`), дефолты встроены
  через `go:embed`, оверрайд — отдельный файл, путь в конфиге (`dispatcher.templates.*`),
  который заменяет дефолт (см. §4).
* **Дайджесты — решено**: реализуются на уровне dispatcher'а (фаза 7), выключены
  по умолчанию, включаются отдельно для каждого канала; формирование — per-channel
  со своим расписанием и шаблоном (см. §4).
