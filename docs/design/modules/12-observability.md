# 12. Observability (`internal/observability`)

## 1. Назначение

Сквозной слой наблюдаемости приложения. Три составляющие:

* **Логирование** — структурные логи через стандартный `log/slog`.
* **Телеметрия приложения** — метрики **Prometheus** (`/metrics` endpoint) по всем модулям пайплайна.
* **Телеметрия LLM** — трассировка LLM-вызовов через **Langfuse** поверх **OpenTelemetry (OTEL)**:
  Langfuse принимает трейсы по стандартному OTLP-протоколу, поэтому `internal/llm` инструментируется
  обычным OTEL SDK, а Langfuse выступает OTLP-бэкендом (генерации, промпты, токены, стоимость).

Как и `storage`, модуль не является шагом пайплайна — им пользуются все остальные модули.

## 2. Ответственность и границы

**Делает:**
* Инициализация и конфигурация `slog` (уровень, формат text/JSON, вывод).
* Обогащение логов контекстом события (`feed_url`, `fingerprint`, `trace_id`) через `slog.Handler`,
  читающий атрибуты из `context.Context`.
* Регистрация и экспорт Prometheus-метрик; HTTP-endpoint `/metrics` (+ `/healthz`, `/readyz`).
* Инициализация OTEL `TracerProvider` с OTLP-экспортёром, направленным в Langfuse;
  graceful flush/shutdown провайдера.
* Хелперы для создания LLM-спанов с Langfuse-семантикой (GenAI semantic conventions:
  `gen_ai.prompt`, `gen_ai.completion`, `gen_ai.usage.*`).

**НЕ делает:**
* Не решает, *что* логировать/мерить — это ответственность каждого модуля.
* Не хранит и не агрегирует метрики (это Prometheus-сервер) и трейсы (это Langfuse).
* Не занимается алертингом (Alertmanager/Grafana — вне приложения).

## 3. Публичный интерфейс

```go
// Инициализация всего слоя наблюдаемости из конфига.
func Init(ctx context.Context, cfg Config) (*Observability, error)

type Observability struct { /* ... */ }

// Logger — корневой slog.Logger; модули получают его через DI и делают logger.With("module", "collector").
func (o *Observability) Logger() *slog.Logger

// Tracer — OTEL-трейсер для LLM-инструментирования (экспорт в Langfuse по OTLP).
func (o *Observability) Tracer() trace.Tracer

// Handler — HTTP-хендлер /metrics + пробы; подключается в cmd/gruh.
func (o *Observability) Handler() http.Handler

// Shutdown — flush OTEL-спанов и корректное завершение.
func (o *Observability) Shutdown(ctx context.Context) error

// Контекстные атрибуты логов: добавляются один раз, попадают во все записи ниже по стеку.
func WithLogAttrs(ctx context.Context, attrs ...slog.Attr) context.Context
```

Метрики модулей — обычные `prometheus.Counter/Histogram/Gauge`, регистрируемые в общем
`prometheus.Registerer`, который передаётся модулям при сборке приложения.

## 4. Внутреннее устройство

### 4.1 Логирование (slog)

* Один корневой `*slog.Logger`, формат по конфигу: `text` (локальная разработка) / `json` (прод).
* Каждый модуль получает логгер через DI и добавляет `slog.With("module", "<имя>")`.
* Кастомный `slog.Handler`-декоратор извлекает атрибуты из `context.Context`
  (положенные `WithLogAttrs`) — так `feed_url`/`fingerprint`/`trace_id` попадают во все логи
  обработки события без ручного проброса.
* Уровни: `debug` — детали HTTP/LLM-запросов; `info` — жизненный цикл события
  (собрано, дедуплицировано, классифицировано, отправлено); `warn` — ретраи, деградации;
  `error` — потеря события, ошибки после всех ретраев.
* Секреты (API-ключи, токены каналов) в логи не попадают никогда; тела фидов и промпты — только на `debug`.

### 4.2 Метрики приложения (Prometheus)

Экспорт через `prometheus/client_golang`, endpoint `/metrics` на отдельном порту (не публичный).

Базовый набор метрик по модулям (префикс `gruh_`):

| Метрика | Тип | Лейблы | Модуль |
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

Кардинальность лейбла `feed` контролируется (число фидов конечное и задаётся конфигом);
при росте числа фидов лейбл заменяется на агрегат.

### 4.3 Телеметрия LLM (Langfuse + OTEL)

* Инструментируется связка `classificator` → `llm`: на каждую классификацию создаётся
  корневой спан (trace), внутри — спаны LLM-вызовов (включая ретраи формата).
* Экспорт — стандартный **OTLP/HTTP** экспортёр OTEL SDK, endpoint **self-hosted** инстанса
  Langfuse (`https://<langfuse-host>/api/public/otel`; облачный cloud.langfuse.com не используется),
  аутентификация — Basic Auth из `public_key`/`secret_key` в заголовке `Authorization`.
* Спаны следуют **OTEL GenAI semantic conventions**, которые Langfuse понимает нативно:
  * `gen_ai.system`, `gen_ai.request.model`, `gen_ai.request.temperature`;
  * `gen_ai.prompt` / `gen_ai.completion` (полные тексты — только если `capture_content: true`);
  * `gen_ai.usage.prompt_tokens` / `gen_ai.usage.completion_tokens`.
* Метаданные трейса: `feed_url`, `fingerprint`, версия промпта (`prompt_version` — `version`
  из YAML-хедера промпта, см. [09-prompt.md](09-prompt.md) §4) — для
  связи качества вердиктов с конкретной версией `classify.md` и построения eval-набора (фаза 7).
* `trace_id` пишется в логи (см. 4.1) — сквозная корреляция «лог ↔ трейс».
* Экспорт асинхронный (`BatchSpanProcessor`); недоступность Langfuse не влияет на пайплайн.

## 5. Зависимости

* stdlib `log/slog`, `net/http`.
* `github.com/prometheus/client_golang` — метрики.
* `go.opentelemetry.io/otel` + `otlptracehttp` — трейсинг LLM (экспорт в Langfuse).

## 6. Конфигурация

```yaml
observability:
  log:
    level: info          # debug | info | warn | error
    format: json         # text | json
  metrics:
    enabled: true
    listen: ":9090"      # отдельный порт для /metrics, /healthz, /readyz
  llm_telemetry:         # Langfuse (self-hosted) через OTEL
    enabled: true
    otlp_endpoint: https://langfuse.internal.example.com/api/public/otel  # только self-hosted инстанс
    capture_content: false   # писать ли полные промпты/ответы в трейсы
    sample_rate: 1.0
# ключи Langfuse: env GRUH_LANGFUSE_PUBLIC_KEY / GRUH_LANGFUSE_SECRET_KEY
```

## 7. Ошибки и крайние случаи

* Наблюдаемость никогда не роняет пайплайн: ошибки экспорта метрик/трейсов логируются на `warn`
  и не пробрасываются наверх.
* Langfuse недоступен — `BatchSpanProcessor` буферизует и дропает по переполнению; счётчик
  дропнутых спанов — в метриках.
* `llm_telemetry.enabled: false` или отсутствие ключей — no-op `TracerProvider`, LLM работает без трейсинга.
* Утечка секретов: промпты/ответы попадают в трейсы только при явном `capture_content: true`;
  API-ключи и токены редактируются на уровне хелперов логирования.
* Shutdown: `Shutdown()` с таймаутом флашит недоотправленные спаны при остановке процесса.

## 8. Тестирование

* Unit: контекстный `slog.Handler` (атрибуты из `ctx` попадают в записи), фильтрация секретов.
* Unit: регистрация метрик без конфликтов, корректность инкрементов через `prometheus/testutil`.
* Unit: LLM-спаны через `tracetest.InMemoryExporter` — правильные GenAI-атрибуты, учёт токенов,
  вложенность спанов ретраев, отсутствие контента при `capture_content: false`.
* Integration (опционально, за build-тегом): отправка тестового трейса в self-hosted Langfuse.

## 9. Открытые вопросы и принятые решения

* **Langfuse Go SDK vs чистый OTEL — решено (чистый OTEL + OTLP)**: вендор-нейтральный
  стандарт, без привязки к SDK; scores/prompt management Langfuse на данном этапе не нужны.
* **`sample_rate` — решено (да)**: параметр `llm_telemetry.sample_rate` входит в конфиг
  с дефолтом `1.0` (все трейсы); снижается при росте объёма/стоимости хранения.
  Семплирование — на уровне корневого спана классификации (`TraceIDRatioBased`),
  чтобы трейс сохранялся или отбрасывался целиком.
* **Границы трейсинга — решено (только LLM и только для Langfuse)**: OTEL-трейсинг
  ограничен связкой `classificator` → `llm` с экспортом в self-hosted Langfuse (см. §4.3);
  на остальной пайплайн не расширяется — наблюдаемость остальных стадий
  обеспечивают метрики Prometheus и логи с `trace_id`-корреляцией.
