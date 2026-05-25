# PoE2PriceChecker — Agent Brief (черновик)

## Цель
Собирать данные рынка Path of Exile 2 (предмет + цена), чтобы научиться оценивать стоимость по свойствам предмета и комбинациям свойств.

## Стек
- Go (основной язык)
- БД: PostgreSQL

## Почему PostgreSQL
Для нашей задачи нужны:
- сложные выборки и JOIN'ы по нормализованным сущностям (item, mods, listing, seller, league/snapshot);
- составные/частичные индексы;
- JSONB для сырого payload и редких полей;
- GIN/GiST + материализованные представления для аналитики;
- оконные функции/CTE для оценки цены по похожим предметам.

Итого: Postgres даст и транзакционную часть ingestion, и удобную аналитическую выборку без смены БД.

## Бинарники
1. `cmd/parser` — отдельный бинарник парсинга (ingestion)
2. `cmd/app` — основной сервис/API/аналитика (позже)

## Parser: базовый контур
- Pull из API рынка PoE2
- Нормализация item/listing/mods
- Запись в Postgres
- Режим периодического опроса + backoff/retry
- Метрики и логирование

## Воркеры
Воркеры конфигурируются поштучно.

Черновой формат конфига:

```yaml
parser:
  workers:
    - name: w1
      proxy: ""           # пусто = без прокси
    - name: w2
      proxy: "socks5://127.0.0.1:9050"
    - name: w3
      proxy: "https://user:pass@host:port"
```

Требования:
- можно задать любое количество воркеров;
- каждому воркеру можно независимо задать proxy или оставить пустым;
- поддержка `socks5://` и `https://`.

## Модель предмета (уровень домена)
- Класс предмета (weapon/armor/flask/jewel/currency/...)
- Базовый тип (template/base)
- Редкость (normal/magic/rare/unique)
- Уровень предмета
- Требования
- Качество
- Сокеты + содержимое
- Состояния (identified/corrupted/mirrored)
- Модификаторы:
  - implicit (база)
  - explicit (основные случайные)
  - rune/socket-derived
  - enchant
  - special tags (crafted/fractured/...)
  - hybrid mods
- Цена листинга (value + currency + timestamp + лига/сезон)

## План следующего шага
1. Зафиксировать формат API-ответов PoE2 (образцы payload).
2. Спроектировать схему БД (DDL v1) под ingestion + pricing queries.
3. Поднять минимальный каркас Go-проекта (go mod, cmd/parser, internal/*).
4. Сделать POC parser worker pool + per-worker proxy transport.
