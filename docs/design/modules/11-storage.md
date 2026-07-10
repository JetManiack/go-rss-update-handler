# 11. Storage (`internal/storage`)

## 1. Назначение

Сквозной слой персистентности на **GORM**: PostgreSQL в проде, SQLite локально и в тестах.
Хранит фиды, обновления, вердикты, историю важных обновлений, маппинг на каналы и статусы доставки.
Единственный модуль, работающий с БД, — остальные видят только интерфейсы-репозитории.

## 2. Ответственность и границы

**Делает:**
* Модели, миграции, репозитории (CRUD + специализированные запросы).
* Атомарную регистрацию fingerprint'ов (уникальный индекс, `ON CONFLICT DO NOTHING`).
* Запрос «N последних важных обновлений фида» для LLM-контекста.
* Хранение `ETag`/`Last-Modified` per feed для collector'а.

**НЕ делает:**
* Не содержит бизнес-логики (границы важности, расписание — не здесь).
* Не кэширует (Redis-кэш — забота вызывающих модулей).

## 3. Публичный интерфейс

```go
type Store interface {
	Feeds() FeedRepo
	Updates() UpdateRepo
	Channels() ChannelRepo
}

type FeedRepo interface {
	List(ctx context.Context) ([]Feed, error)
	UpdateCacheHeaders(ctx context.Context, feedID int64, etag, lastModified string) error
	ChannelsFor(ctx context.Context, feedID int64) ([]string, error)
}

type UpdateRepo interface {
	// InsertNew атомарно вставляет обновления, возвращая только реально новые (дедупликация).
	InsertNew(ctx context.Context, updates []Update) ([]Update, error)
	SaveVerdict(ctx context.Context, updateID int64, v Verdict) error
	LastImportant(ctx context.Context, feedID int64, n int) ([]Update, error)
	MarkDispatched(ctx context.Context, updateID int64, channel string) error
}
```

## 4. Внутреннее устройство

### Схема данных

```
feeds
  id, url (unique), etag, last_modified, active, created_at
  # интервалы опроса в БД не хранятся — они в конфиге (см. 13-config.md)

updates
  id, feed_id (fk), fingerprint (unique), source_url,
  published_at, created_at,
  verdict_important (nullable), verdict_category, verdict_confidence,
  verdict_reason, classified_at
  # fingerprint и вердикт хранятся вечно (см. §9), raw_content вынесен в raw_contents

raw_contents             # сырой контент вынесен отдельно для retention-политики
  update_id (fk, PK), content (TEXT), created_at

channels
  id, name (unique), type, config_json

feed_channels            # маппинг Feed URL -> каналы
  feed_id (fk), channel_id (fk), PK(feed_id, channel_id)

dispatches               # факты доставки (идемпотентность)
  update_id (fk), channel_id (fk), delivered_at, PK(update_id, channel_id)
```

* «Важное обновление» = `updates.verdict_important = true` — отдельная таблица не нужна;
  `LastImportant` — запрос с `ORDER BY published_at DESC LIMIT n`.
* `raw_contents` — 1:1 к `updates`; строки `updates` (fingerprint + вердикт) живут вечно,
  а тяжёлый сырой контент чистится retention-джобой по `raw_contents.created_at`
  (порог — `storage.raw_content_retention` в конфиге, `0` = хранить вечно).
* Индексы: `updates(feed_id, verdict_important, published_at)`, `updates(fingerprint) unique`.
* Миграции: старт с GORM `AutoMigrate`; переход на версионированные (golang-migrate) при первом breaking-изменении схемы.
* Диалект выбирается по DSN конфига; вся логика запросов совместима с обоими диалектами
  (ON CONFLICT поддержан и в PostgreSQL, и в SQLite).

## 5. Зависимости

* `gorm.io/gorm`, `gorm.io/driver/postgres`, `gorm.io/driver/sqlite`.

## 6. Конфигурация

```yaml
storage:
  driver: postgres            # postgres | sqlite
  dsn: env GRUH_DB_DSN        # секреты из env
  max_open_conns: 10
  log_queries: false
  raw_content_retention: 90d  # срок хранения raw_contents; 0 = вечно
```

## 7. Ошибки и крайние случаи

* Конфликт fingerprint при конкурентной вставке — не ошибка: `InsertNew` возвращает запись как «не новую».
* Недоступность БД — типизированные ошибки наверх; ретраи и политика — на вызывающей стороне.
* Большой сырой контент — тип `TEXT` в `raw_contents`, лимит контролируется parser'ом.
* Обращение к уже вычищенному `raw_contents` (контент удалён retention'ом) — валидный случай:
  репозиторий возвращает «контента нет», вызывающий код обязан это учитывать.
* SQLite и конкуренция — `busy_timeout`, WAL-режим; в распределённом режиме SQLite запрещён (валидация конфига).
* Рост объёма данных — контролируется retention-политикой `raw_contents` (решено, см. §9).

## 8. Тестирование

* Репозиторные тесты на SQLite in-memory (быстро, без инфраструктуры).
* Прогон того же набора на PostgreSQL через testcontainers в CI (совместимость диалектов).
* Обязательные сценарии: конкурентный `InsertNew`, `LastImportant` с граничными n, идемпотентный `MarkDispatched`.

## 9. Открытые вопросы и принятые решения

* **Источник истины — решено (БД)**: фиды, каналы (`config_json`) и маппинг
  `feed_channels` живут в БД; пока управление — напрямую в БД (seed/SQL-скрипты),
  отдельным шагом будут разработаны управляющие транспорты — Slack/Telegram-бот
  (фаза 7); CLI-команд управления нет (CLI — одна рут-команда, целимся в k8s —
  ручные команды лишние, см. [00-overview.md](../00-overview.md) §7). Интервалы
  опроса и технические параметры — в конфиге. См. [13-config.md](13-config.md).
* **Недоступность БД — решено (fail fast)**: БД — обязательная зависимость,
  приложение выдаёт ошибку и падает (см. [04-deduplicator.md](04-deduplicator.md) §9).
* **Retention — решено**: сырой контент вынесен в отдельную таблицу `raw_contents`
  (1:1 к `updates`), и его хранением управляет retention-политика
  (`storage.raw_content_retention`, периодическое удаление старых строк).
  Строки `updates` — **fingerprint и вердикт — хранятся вечно**: fingerprint'ы нужны
  для дедупликации (удаление → риск повторных уведомлений о старых записях фида),
  вердикты — для LLM-контекста и eval (фаза 7).
