# GRUH — Роадмап разработки

Роадмап разбит на фазы; каждая фаза завершается работоспособным инкрементом.
Порядок реализации модулей выбран так, чтобы как можно раньше получить сквозной
вертикальный срез (feed → парсинг → дедупликация → уведомление), а LLM-классификацию
и распределённость добавлять поверх стабильного ядра.

## Процесс разработки

Все фазы реализуются по единому процессу:

* **TDD** — каждая фича начинается с падающего теста: red → green → refactor.
  Пункт чек-листа считается выполненным только при наличии тестов, покрывающих его поведение.
* **GitFlow (feature branches)** — на каждую фичу (обычно один пункт чек-листа) создаётся
  отдельная ветка `feature/<краткое-имя>` от `main`. После прохождения всех тестов
  (локально и в CI) ветка мерджится в `main`. `main` всегда в зелёном состоянии.

## Фаза 0 — Каркас проекта

**Цель:** собираемый бинарь и инфраструктура разработки.

> Предусловие: git-репозиторий инициализируется владельцем проекта до начала разработки
> (`git init`, первый коммит с документацией, ветка `main`).

- [ ] Инициализация модуля, layout каталогов (`cmd/gruh`, `internal/*`, `deploy/`, `docs/`)
- [ ] CLI-скелет на `urfave/cli/v3`: **одна рут-команда** `gruh` = запуск сервиса
  (функции serve), флаги `--config`, `--version` и `--check-config` (валидация конфига
  без запуска); отдельных команд `serve`/`migrate`/`version` нет,
  сабкоманды зарезервированы под роли микросервисов (фаза 5)
- [ ] `internal/config`: загрузка конфигурации (koanf: YAML + env, приоритет env), fail-fast валидация, `config.example.yaml`
- [ ] `internal/observability` (базовая часть): логирование `log/slog` (уровни, text/JSON, контекстные атрибуты), graceful shutdown по сигналам
- [ ] `Makefile` (build, lint, test), `golangci-lint`, CI — **GitHub Actions** (`.github/workflows/ci.yml`: build + lint + `go test ./...` на PR и `main`)
- [ ] `docker-compose` для локальной разработки (PostgreSQL, Redis)

**Документы:** [12-observability.md](modules/12-observability.md), [13-config.md](modules/13-config.md)

**Выход:** `gruh --version` работает, окружение поднимается одной командой.

## Фаза 1 — Хранилище и модель данных

**Цель:** слой персистентности, на который опираются все остальные модули.

- [ ] `internal/storage`: модели GORM (Feed, Update, RawContent, Channel, FeedChannelMapping, Dispatch);
  fingerprint/вердикт хранятся вечно, сырой контент — в `raw_contents` с retention-политикой
- [ ] Репозитории + миграции (AutoMigrate / версионированные) — выполняются автоматически
  при старте рут-команды (fail fast при ошибке), отдельной команды `migrate` нет
- [ ] Поддержка PostgreSQL (прод) и SQLite (локально/тесты)
- [ ] БД — источник истины для фидов/каналов/маппинга; пока управление — напрямую в БД
  (seed/SQL-скрипты); управляющие транспорты (Slack/Telegram-бот) — отдельным шагом (фаза 7);
  интервалы опроса — в конфиге

**Документы:** [11-storage.md](modules/11-storage.md), [13-config.md](modules/13-config.md)

## Фаза 2 — Сбор и парсинг (вертикальный срез без LLM)

**Цель:** система опрашивает фиды и складывает новые уникальные обновления в БД.

- [ ] `internal/scheduler`: интервальный планировщик с jitter
- [ ] `internal/collector`: HTTP-клиент с `ETag` / `If-Modified-Since`, rate limiting, retry/backoff
- [ ] `internal/model`: общий тип `UpdateEvent` (принятое решение, см. [03-parser.md](modules/03-parser.md) §9)
- [ ] `internal/parser`: gofeed → единый `UpdateEvent` (semver-тег не извлекается — это зона classificator'а)
- [ ] `internal/deduplicator`: fingerprinting, отсечение дубликатов
- [ ] In-memory реализация `internal/bus` (интерфейс шины фиксируется здесь;
  топики-константы `updates.new` / `updates.classified`, версия схемы — в конверте `Message`,
  см. [05-bus.md](modules/05-bus.md) §9)
- [ ] Базовый `internal/orchestrator`: связывание шагов пайплайна

**Документы:** [01-scheduler.md](modules/01-scheduler.md), [02-collector.md](modules/02-collector.md),
[03-parser.md](modules/03-parser.md), [04-deduplicator.md](modules/04-deduplicator.md),
[05-bus.md](modules/05-bus.md), [06-orchestrator.md](modules/06-orchestrator.md)

**Выход:** монолитный `gruh` (рут-команда) наполняет БД новыми обновлениями GitHub Atom-фидов.

## Фаза 3 — LLM-классификация

**Цель:** отделение важных обновлений от шума.

- [ ] `internal/llm`: OpenAI-совместимый клиент (таймауты, retry, учёт токенов)
- [ ] `internal/prompt`: встроенные промпты через `go:embed`, переопределение из пользовательской директории, Go templates;
  YAML-хедер промпта (`name`/`version`/`critical`/`description`, см. [09-prompt.md](modules/09-prompt.md) §4)
- [ ] `internal/classificator`: вердикт важности; контекст = текущее обновление + 2 последних важных;
  порог уверенности 0.5, security-патчи всегда важны (правило поверх LLM)
- [ ] Сохранение вердиктов и истории важных обновлений в `storage`
- [ ] Недоступность LLM (после ретраев) — fail fast: ошибка и падение без сохранения
  состояния классификации (fallback между моделями — на стороне LiteLLM, не в приложении)
- [ ] LLM-телеметрия: трейсы классификации в **Langfuse** через OTEL/OTLP (GenAI-атрибуты, токены, версия промпта)

**Документы:** [07-classificator.md](modules/07-classificator.md), [08-llm.md](modules/08-llm.md),
[09-prompt.md](modules/09-prompt.md), [12-observability.md](modules/12-observability.md)

**Выход:** каждое новое обновление получает вердикт important/noise с объяснением.

## Фаза 4 — Доставка уведомлений

**Цель:** пользователи получают уведомления о важных обновлениях.

- [ ] `internal/dispatcher`: общий интерфейс `Notifier`
- [ ] Реализации: Webhook → Slack → Telegram
- [ ] Шаблоны текста уведомлений: Go template, дефолты через `go:embed`,
  оверрайд файлом из конфига (`dispatcher.templates.*`)
- [ ] Маршрутизация по маппингу `Feed URL -> каналы`
- [ ] Retry-политика доставки и защита от повторной отправки

**Документы:** [10-dispatcher.md](modules/10-dispatcher.md)

**Выход:** полный MVP-цикл: фид → классификация → уведомление в канал.

## Фаза 5 — Распределённый режим (Redis)

**Цель:** горизонтальное масштабирование в k8s.

- [ ] Redis-реализация `internal/bus` (Streams + consumer groups, ack/retry, DLQ)
- [ ] Разделение ролей процессов: collector / worker(classificator) / dispatcher —
  здесь появляются сабкоманды `gruh collector | worker | dispatcher` (рут-команда без сабкоманды = монолит)
- [ ] Гарантии идемпотентности при повторной доставке из шины
- [ ] Распределённые блокировки планировщика (несколько реплик scheduler)

**Документы:** [05-bus.md](modules/05-bus.md), [06-orchestrator.md](modules/06-orchestrator.md)

## Фаза 6 — Деплой и эксплуатация

**Цель:** production-ready развёртывание.

- [ ] `Dockerfile` (multi-stage, distroless)
- [ ] Helm-чарты в `deploy/`, `skaffold.yaml`
- [ ] HPA по метрикам очереди/CPU
- [ ] Метрики Prometheus по всем модулям (`/metrics`), health/readiness-пробы
- ОТКЛОНЕНО: трейсинг всего пайплайна — OTEL-трейсинг остаётся только для LLM
  и только для Langfuse (принятое решение, см. [12-observability.md](modules/12-observability.md) §9)

**Документы:** [12-observability.md](modules/12-observability.md)

## Фаза 7 — Развитие (backlog)

- [ ] Управляющие транспорты для фидов и каналов — Slack/Telegram-бот (команды добавления/списка/удаления
  фидов прямо из мессенджера); опционально — Web UI / API
- [ ] Дайджесты (агрегация нескольких обновлений в одно уведомление): выключены
  по умолчанию, включаются и формируются отдельно для каждого канала
  (расписание и шаблон per-channel, см. [10-dispatcher.md](modules/10-dispatcher.md) §4)
- [ ] Retention-джоба для `raw_contents` (очистка старого сырого контента,
  см. [11-storage.md](modules/11-storage.md) §9)
- [ ] Дополнительные типы источников (не-GitHub RSS, changelog-страницы)
- [ ] Оценка качества классификации (feedback loop, разметка ложных срабатываний)
- [ ] Кэширование/бюджетирование LLM-вызовов

## Зависимости фаз

```
Фаза 0 ──▶ Фаза 1 ──▶ Фаза 2 ──▶ Фаза 3 ──▶ Фаза 4 ──▶ Фаза 5 ──▶ Фаза 6 ──▶ Фаза 7
                        (MVP-ядро)  (интеллект)  (MVP)     (масштаб)  (prod)
```

Фазы 5 и 6 могут выполняться параллельно после завершения фазы 4.
