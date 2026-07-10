# GRUH — Дизайн-документация

Иерархия дизайн-документов проекта **GRUH (Go RSS Update Handler)** — AI-powered системы обработки RSS/Atom-фидов.
Верхнеуровневое описание архитектуры — в [00-overview.md](00-overview.md).

## Структура документации

```
docs/design/
├── README.md          # Этот файл — индекс документации
├── 00-overview.md     # Общий архитектурный обзор, потоки данных, сквозные решения
├── ROADMAP.md         # Роадмап разработки по фазам
└── modules/           # Дизайн-документы по каждому модулю internal/
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

Нумерация модулей соответствует порядку прохождения события по пайплайну
(scheduler → collector → parser → deduplicator → bus → orchestrator → classificator/llm/prompt → dispatcher),
модули `storage`, `observability` и `config` — сквозные и описаны последними.

## Документы

| № | Документ | Модуль | Назначение |
|---|----------|--------|------------|
| — | [00-overview.md](00-overview.md) | — | Общая архитектура, пайплайн, модель данных, стек |
| 1 | [01-scheduler.md](modules/01-scheduler.md) | `internal/scheduler` | Планирование задач опроса фидов с jitter |
| 2 | [02-collector.md](modules/02-collector.md) | `internal/collector` | HTTP-загрузка фидов, rate limiting, ETag |
| 3 | [03-parser.md](modules/03-parser.md) | `internal/parser` | Парсинг RSS/Atom/JSON в единый формат |
| 4 | [04-deduplicator.md](modules/04-deduplicator.md) | `internal/deduplicator` | Fingerprinting и проверка уникальности |
| 5 | [05-bus.md](modules/05-bus.md) | `internal/bus` | Единая шина событий (Redis) |
| 6 | [06-orchestrator.md](modules/06-orchestrator.md) | `internal/orchestrator` | Управление потоком обработки события |
| 7 | [07-classificator.md](modules/07-classificator.md) | `internal/classificator` | LLM-классификация важности обновления |
| 8 | [08-llm.md](modules/08-llm.md) | `internal/llm` | OpenAI-совместимый клиент |
| 9 | [09-prompt.md](modules/09-prompt.md) | `internal/prompt` | Управление промптами (.md + Go templates) |
| 10 | [10-dispatcher.md](modules/10-dispatcher.md) | `internal/dispatcher` | Доставка уведомлений (Slack, Telegram, Webhook) |
| 11 | [11-storage.md](modules/11-storage.md) | `internal/storage` | Слой репозиториев (GORM: PostgreSQL/SQLite) |
| 12 | [12-observability.md](modules/12-observability.md) | `internal/observability` | Логирование (slog), метрики (Prometheus), LLM-телеметрия (Langfuse + OTEL) |
| 13 | [13-config.md](modules/13-config.md) | `internal/config` | Загрузка и валидация конфигурации (YAML + env) |
| — | [ROADMAP.md](ROADMAP.md) | — | Фазы разработки и порядок реализации |

## Процесс разработки

Проект разрабатывается по следующим правилам (обязательны для всех фаз и модулей):

* **TDD (Test-Driven Development)** — сначала пишется падающий тест, затем минимальная
  реализация, затем рефакторинг (цикл red → green → refactor). Код без тестов не мерджится.
* **GitFlow (feature branches)** — на каждую фичу создаётся отдельная ветка
  (`feature/<краткое-имя>`); после прохождения всех тестов и ревью ветка мерджится в `main`.
  Ветка `main` всегда остаётся в рабочем (зелёном) состоянии.

Подробнее см. раздел «Процесс разработки» в [ROADMAP.md](ROADMAP.md).

## Соглашения по документам

Каждый дизайн-документ модуля следует единому шаблону:

1. **Назначение** — зачем модуль нужен и его место в пайплайне.
2. **Ответственность и границы** — что модуль делает и что явно НЕ делает.
3. **Публичный интерфейс** — предполагаемые Go-интерфейсы и типы.
4. **Внутреннее устройство** — ключевые решения и алгоритмы.
5. **Зависимости** — от каких модулей/библиотек зависит.
6. **Конфигурация** — параметры из `config.yaml`.
7. **Обработка ошибок и крайние случаи**.
8. **Тестирование** — стратегия тестирования модуля.
9. **Открытые вопросы** — что требует уточнения перед реализацией.
