# 03. Parser (`internal/parser`)

## 1. Назначение

Преобразует сырое содержимое фида (RSS 2.0 / Atom / JSON Feed) в унифицированный внутренний
формат — список `UpdateEvent`. Скрывает от остального пайплайна различия форматов.

## 2. Ответственность и границы

**Делает:**
* Автоопределение формата и парсинг через `gofeed`.
* Маппинг записей фида в `UpdateEvent` (SourceURL, RawContent, PublishedAt).
* Нормализация: временные зоны → UTC, очистка/усечение HTML-контента, выбор наиболее
  информативного поля (content > description > title).

**НЕ делает:**
* Не проверяет уникальность (это `deduplicator`).
* Не вычисляет fingerprint (это `deduplicator`), но предоставляет стабильные поля для него.
* Не выполняет сетевых запросов.

## 3. Публичный интерфейс

```go
type Parser interface {
	// Parse разбирает сырое тело фида и возвращает события в порядке публикации (новые первыми).
	Parse(ctx context.Context, feedURL string, body []byte) ([]UpdateEvent, error)
}

// UpdateEvent — ядро модели данных системы; определён в пакете `internal/model`
// (принятое решение, см. §9) и используется parser, bus, deduplicator, storage.
type UpdateEvent struct {
	SourceURL   string    // URL записи (link) либо URL фида
	RawContent  string    // нормализованный контент записи
	PublishedAt time.Time // UTC
	Fingerprint string    // заполняется deduplicator'ом
}
```

## 4. Внутреннее устройство

* `gofeed.Parser` с `ParseString`/`Parse` — универсален для RSS/Atom/JSON.
* Для GitHub Atom: запись = релиз/тег; `SourceURL` = `entry.Link`, контент = `entry.Content`
  (release notes в HTML), заголовок содержит имя тега.
* Fallback для дат: `Published` → `Updated` → время загрузки (с пометкой).
* Ограничение размера `RawContent` (например, 64 KiB) для защиты LLM-контекста и БД.

## 5. Зависимости

* `github.com/mmcdole/gofeed`.
* `internal/model` — общий тип `UpdateEvent` (принятое решение, см. §9).

## 6. Конфигурация

```yaml
parser:
  max_content_size: 64KiB
  strip_html: false   # сохранять ли HTML в RawContent (LLM неплохо читает HTML/markdown)
```

## 7. Ошибки и крайние случаи

* Невалидный XML/JSON — ошибка парсинга наверх, фид помечается проблемным после N подряд неудач.
* Пустой фид (нет записей) — не ошибка, пустой результат.
* Записи без даты/ссылки — заполнение fallback-значениями, событие не теряется.
* Нестандартные кодировки — gofeed конвертирует; при неудаче — ошибка с диагностикой.
* Дубликаты записей внутри одного фида — отдать все, отсечёт `deduplicator`.

## 8. Тестирование

* Golden-файлы: реальные примеры GitHub Atom, RSS 2.0, JSON Feed в `testdata/`.
* Крайние случаи: пустой фид, битый XML, отсутствующие даты, огромный контент.

## 9. Открытые вопросы и принятые решения

* **Место определения `UpdateEvent` — решено (`internal/model`)**: тип нужен сразу
  нескольким модулям (parser, bus, deduplicator, storage, dispatcher), поэтому живёт
  в отдельном пакете `internal/model` без зависимостей — ни один модуль не тянет parser
  ради типа.
* **Semver-тег GitHub — решено (зона classificator'а)**: парсер не извлекает
  структурированные поля GitHub; заголовок/контент с именем тега попадают в `RawContent`
  как есть, интерпретация версии (major/minor/patch, security) — задача LLM-классификации.
