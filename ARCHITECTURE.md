# ARCHITECTURE.md — PoE2PriceChecker (draft)

## Scope v1
- Источник листингов: GGG Trade2 API (`search` + `fetch`)
- Фокус: рандомные/редкие предметы и их моды/роллы
- Уники — вне scope v1

## Secrets
- `POESESSID` хранится только в env
- В репозиторий секреты не коммитим

## Data model boundary
- `ref.*` (PostgreSQL schema): внешние справочники RePoE-fork
- `market.*`: экземпляры предметов, листинги, цены, снапшоты

## Parser binary
- Отдельный бинарник: `cmd/parser`
- Worker pool, у каждого воркера свой HTTP client/transport
- У каждого воркера отдельная proxy-настройка (none/socks5/https)

## Rate-limit policy (critical)
- **Лимит на воркера, НЕ глобальный**
- Каждый воркер имеет собственный limiter (token bucket)
- Настраиваются отдельно:
  - `search_rps`, `search_burst`
  - `fetch_rps`, `fetch_burst`
- При `429`/`5xx`: exponential backoff + jitter
- При серии ошибок: временная пауза воркера (cooldown)
- Safe default: консервативный RPS

## Execution model
1. Worker -> `search`
2. Полученные ids батчами -> `fetch`
3. Нормализация + связывание с `ref.*`
4. Upsert в `market.*`
5. Метрики/логирование

## Notes
- Если нужен максимально безопасный режим — запуск с одним воркером.
- Масштабирование: добавляем воркеры (с их proxy и их лимитами).
