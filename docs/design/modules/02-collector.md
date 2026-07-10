# 02. Collector (`internal/collector`)

## 1. Назначение

Загружает сырое содержимое фида по HTTP по задаче от планировщика. Обязан использовать
условные запросы (`ETag` / `If-Modified-Since`) — критично для GitHub, который активно
поддерживает 304 и rate limits.

## 2. Ответственность и границы

**Делает:**
* HTTP GET с условными заголовками (`If-None-Match: <etag>`, `If-Modified-Since`).
* Хранит/обновляет `ETag` и `Last-Modified` по каждому фиду (через `storage`).
* Rate limiting per-host (например, `golang.org/x/time/rate`).
* Retry с экспоненциальным backoff на сетевые ошибки и 5xx; уважение `Retry-After` при 429.
* Возвращает сырой контент (`[]byte`) и метаданные ответа.

**НЕ делает:**
* Не парсит содержимое (это `parser`).
* Не решает, когда опрашивать (это `scheduler`).

## 3. Публичный интерфейс

```go
type Collector interface {
	// Fetch возвращает результат загрузки; NotModified == true при 304.
	Fetch(ctx context.Context, feed FeedRef) (FetchResult, error)
}

type FeedRef struct {
	FeedID       int64
	URL          string
	ETag         string
	LastModified string
}

type FetchResult struct {
	NotModified  bool
	Body         []byte
	ETag         string
	LastModified string
	FetchedAt    time.Time
}
```

## 4. Внутреннее устройство

* Один переиспользуемый `http.Client` с таймаутами (connect, total) и лимитом размера тела ответа.
* Per-host `rate.Limiter`, хранимый в map с мьютексом (host извлекается из URL).
* Кастомный `User-Agent` (`gruh/<version>`), поддержка редиректов с ограничением.
* Условные заголовки: если сервер вернул 304 — результат `NotModified`, тело не читается,
  новые ETag/Last-Modified при наличии сохраняются.

## 5. Зависимости

* `internal/storage` — персистентность ETag/Last-Modified (через вызывающую сторону или интерфейс).
* stdlib `net/http`, `golang.org/x/time/rate`.

## 6. Конфигурация

```yaml
collector:
  timeout: 30s
  max_body_size: 5MiB
  user_agent: "gruh/1.0"
  rate_limit_per_host: 1rps
  retries: 3
  backoff_base: 2s
```

## 7. Ошибки и крайние случаи

* `429 Too Many Requests` — уважать `Retry-After`, снижать частоту (сигнал планировщику).
* `404/410` — фид умер: пометить в storage, уведомить (не ретраить бесконечно).
* Слишком большое тело / не-XML контент — ошибка с диагностикой, без падения процесса.
* Редиректы 301 — опционально обновлять URL фида в storage.
* Обрыв соединения посреди тела — retry с backoff.

## 8. Тестирование

* Unit с `httptest.Server`: сценарии 200/304/404/429/5xx, проверка условных заголовков.
* Проверка rate limiter (несколько URL одного хоста), ограничения размера тела, ретраев.

## 9. Принятые решения (бывшие открытые вопросы)

* **Сигнал для adaptive polling — да**: adaptive polling принят (см. [01-scheduler.md](01-scheduler.md) §2);
  collector возвращает результат опроса (`NotModified`/ошибка/новый контент) в `FetchResult`,
  планировщик использует его для backoff «тихих» фидов.
* **HTTP-прокси / кастомные CA — нет**: не поддерживаются и не планируются
  (используется системный пул CA и прямое соединение).
