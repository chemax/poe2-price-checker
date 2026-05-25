# PoE2PriceChecker

Черновой сервис для сбора рынка Path of Exile 2 и оценки цены редких предметов по модам/роллам.

## Что уже есть

- Go-проект с отдельными бинарниками:
  - `cmd/parser` — ingestion из Trade2 API
  - `cmd/refsync` — загрузка справочников RePoE-fork в `ref.*`
  - `cmd/migrate` — миграции БД
- PostgreSQL в `docker-compose`
- Миграции схем:
  - `ref.*` — справочники
  - `market.*` — рыночные экземпляры/листинги
- Per-worker конфиг:
  - отдельные proxy (`none/socks5/https`)
  - отдельные rate limits (`search`/`fetch`)
  - retry/backoff/cooldown
- Поддержка нескольких search queries (`parser.queries[]`)
- Базовая аналитика по query:
  - `market.v_query_runs`
  - `market.v_query_stats_24h`

## Быстрый старт

```bash
make db-up
make migrate-up
make ref-sync
```

Запуск parser:

```bash
export POESESSID=...
go run ./cmd/parser -config config.example.yaml
```

## Конфиг

Смотри `config.example.yaml`.

Критично:
- `postgres.dsn`
- `parser.league`
- `parser.queries` (минимум 1)
- `parser.workers` (минимум 1)
- `POESESSID` через env

## Ветки

Текущая рабочая ветка: `feature/codex`.
В `main` сливаемся по вехам.
