# Hash diagnostics (2026-05-25)

## Scope
Проверка перед rule-layer:
1) hash presence в payload vs извлечение в `market.item_mods`
2) hash coverage в `ref.trade2_stats`
3) размер костыля overrides
4) статус rune

## 1) Payload -> item_mods extraction (10 random items)
Итог по выборке:
- Для `explicit/implicit/desecrated/enchant` извлечение корректное: `item_mods_cnt` соответствует индексным ссылкам в `extended.hashes.*`, `with_stat_hash = item_mods_cnt`.
- Для `rune` в payload hashes идут как `[["rune.stat_xxx", null], ...]`, то есть без индексов.
- Для `rune` в БД закономерно `with_stat_hash = 0`.
- `fractured` присутствует в payload, но в `item_mods` сейчас не пишется (по выборке `item_mods_cnt=0`).

Вывод: потеря hash массово не в парсере explicit/implicit/desecrated/enchant; узкое место — структура rune (и отдельно fractured ingestion gap).

## 2) Payload hash coverage in ref.trade2_stats
Срез по уникальным hash из текущих payload:
- total_payload_hashes: 79
- exact: 34 (43.04%)
- override: 26
- fuzzy: 0
- unmapped_or_missing: 19

Вывод: справочник системно неполный (exact < 50%).

## 3) Overrides state
- `map_method='override'` в `ref.trade2_stats`: 26
- used overrides in current `market.item_mods`: 26

Вывод: overrides уже «десятки», это временная мера, не масштабируется.

## 4) Rune status
Подтверждено на сыром JSON: `extended.hashes.rune` приходит без индексов (`null`), прямая line-binding модель неприменима.

## Practical conclusions
1. До универсального rule-layer надо чинить справочник и ingest strategy.
2. Для rune нужна отдельная ветка логики (по index связать нельзя).
3. Нужен альтернативный или дополняющий источник статов (PoE2 bundle/GGPK pipeline).
4. Rule-layer допустим только как узкий и маркированный (`match_method='rule'`) для подтверждённо безhash-кластеров.

## Candidate external source for PoE2 data
- https://github.com/juddisjudd/ggpk-explorer (PoE2 bundle/GGPK explorer)

(найдено web search; требует отдельной валидации и PoC)
