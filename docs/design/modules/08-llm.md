# 08. LLM (`internal/llm`)

## 1. Назначение

Тонкий транспортный клиент к **OpenAI-совместимому API** (OpenAI, Azure OpenAI, Ollama,
vLLM, OpenRouter и т.п.). Изолирует остальную систему от деталей конкретного провайдера.

## 2. Ответственность и границы

**Делает:**
* Chat completions запросы (`POST /v1/chat/completions`) с поддержкой JSON mode.
* Таймауты, ретраи с backoff на 429/5xx, уважение `Retry-After`.
* Учёт использования токенов (метрики: prompt/completion tokens, latency, стоимость).
* Инструментирование: Prometheus-метрики (`gruh_llm_*`) и OTEL-спаны на каждый запрос
  с GenAI-атрибутами (экспорт в Langfuse) — см. [12-observability.md](12-observability.md).
* Ограничение параллельных запросов (семафор) — защита бюджета и rate limits провайдера.

**НЕ делает:**
* Не строит промпты и не интерпретирует ответы (это `classificator` + `prompt`).
* Не кэширует ответы (возможное развитие, фаза 7).

## 3. Публичный интерфейс

```go
type Client interface {
	Complete(ctx context.Context, req Request) (Response, error)
}

type Request struct {
	System      string
	User        string
	JSONMode    bool    // требовать application/json ответ
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

## 4. Внутреннее устройство

* **Реализация поверх `net/http` (принято, см. §9)**: провайдер — vLLM/LiteLLM,
  нужны только chat completions + JSON mode; формат OpenAI-совместимого API стабилен и прост.
* `base_url` конфигурируем → работает с любым совместимым провайдером, включая локальные.
* Ретраи: только идемпотентные ошибки (сетевые, 429, 5xx); 4xx (кроме 429) — фатально сразу.
* API-ключ — только из переменной окружения (`GRUH_LLM_API_KEY`), в конфиге не хранится.

## 5. Зависимости

* stdlib `net/http`, `encoding/json` — без внешних LLM-SDK (см. §9).
* `internal/observability` — метрики и OTEL-трейсер для LLM-телеметрии (Langfuse).

## 6. Конфигурация

```yaml
llm:
  base_url: http://litellm.internal:4000/v1   # vLLM / LiteLLM (OpenAI-совместимый endpoint)
  model: gpt-4o-mini
  timeout: 60s
  max_retries: 3
  max_concurrent: 4
  temperature: 0.1     # классификация — низкая температура
# api-ключ: env GRUH_LLM_API_KEY
```

## 7. Ошибки и крайние случаи

* 429 / quota exceeded — backoff с уважением `Retry-After`; после исчерпания ретраев —
  ошибка наверх и **fail fast**: процесс падает без сохранения состояния классификации
  (см. §9 и [06-orchestrator.md](06-orchestrator.md) §7).
* Контекст модели превышен (`context_length_exceeded`) — специализированная ошибка, по которой classificator усечёт вход.
* Обрыв соединения / таймаут — retry.
* Ответ без `choices` или с `finish_reason: length` — ошибка с диагностикой.

## 8. Тестирование

* Unit с `httptest.Server`, мимикрирующим OpenAI API: успех, 429 c Retry-After, 5xx, битый JSON, JSON mode.
* Опциональный smoke-тест против реального провайдера за build-тегом (`//go:build llm_live`).

## 9. Открытые вопросы и принятые решения

* **`net/http` vs `openai-go` — решено: своя реализация на `net/http`.**
  Обоснование для провайдеров vLLM/LiteLLM:
  * используется один эндпоинт (`/v1/chat/completions` + JSON mode) — это ~200 строк кода,
    полный SDK избыточен;
  * `openai-go` развивается вслед за облачным OpenAI API (Responses API, новые поля) —
    OpenAI-совместимые прокси часто отстают в поддержке новых полей/строгой валидации,
    тонкий клиент отправляет ровно то, что они понимают;
  * полный контроль над ретраями/таймаутами/семафором и OTEL-инструментированием
    без обёрток вокруг чужого transport;
  * меньше зависимостей, тривиальное тестирование через `httptest.Server`.
  Если понадобятся streaming/tools — решение можно пересмотреть, интерфейс `Client` это допускает.
* **Fallback между моделями — решено (в приложении не нужен)**: при необходимости
  fallback настраивается на стороне LiteLLM-прокси. Если модель недоступна
  (после исчерпания ретраев) — **ошибка и падение без сохранения состояния
  классификации** — аналогично недоступности БД (fail fast,
  см. [11-storage.md](11-storage.md) §9); после рестарта события обрабатываются заново.
