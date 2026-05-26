# Architecture audit (2026-05-26)

## 1. Архитектурные смешения

**Matcher знает структуру trade2 payload напрямую.**
`extractItemHashes` в `modmatcher.go:198–273` парсит `extended.hashes` и
`extended.mods[*].magnitudes[*].hash` прямо из raw item payload. Это знание
о специфике GGG trade2 API живёт внутри matcher, хотя должно быть изолировано
в `internal/trade2`. Mapper добросовестно сохраняет полный payload в
`market.items.payload`, но хэши в `market.item_mods` не материализует —
они каждый раз перечитываются из JSON в matcher. Если GGG изменит формат
`extended`, сломаются оба слоя, но искать причину придётся в matcher.

**`resolvePriceDivine` внутри транзакции upsert (`repository.go:165–204`).**
Функция делает SELECT к `market.currency_rates` внутри транзакции хранения
листинга. Это pricing-логика в storage-слое. Если курсов нет — `price_divine`
тихо становится NULL без логирования.

**`loadStatPatterns` / `loadValidModStatIDs` дублированы дословно** в
`trade2stats/sync.go:127–185` и `matcher/modmatcher.go:448–505`. Два
независимых источника одной логики — шанс на расхождение при будущих
изменениях.

---

## 2. Проблема справочника

**Вывод: причина (c) + частично (b).**

По диагностическому отчёту: из 79 уникальных хэшей в payload — 34 exact
(43%), 26 override, 0 fuzzy, 19 unmapped. Парсер хэши не теряет, так что
(a) исключено.

`refsync/sync.go` тянет ровно 6 файлов: `base_items`, `item_classes`, `tags`,
`mods`, `mods_by_base`, `stat_translations`. Это исчерпывающий набор для
text-based mapping, ничего полезного от RePoE-fork не упускается.

**Тонкий баг: `normalizeLine` в `trade2stats/sync.go:218–237` и в
`matcher/modmatcher.go:600–622` — разные функции.** В matcher есть строчка:

```go
// modmatcher.go:603
if i := strings.Index(s, ":"); i >= 0 && i < 40 {
    s = strings.TrimSpace(s[i+1:])
}
```

В trade2stats этого нет. Итог: `normalized_text` в БД вычислен с другим
алгоритмом нормализации. Фактически `normalized_text` — мёртвая колонка;
используется `trade_text`, который ре-нормализуется runtime в matcher. Явных
ошибок нет, но maintenance-ловушка.

Настоящая причина 57% miss — `stat_translations.json` из RePoE-fork неполон
для PoE2. Числовые хэши в GGG trade2 API не имеют named stat_id в RePoE,
а threshold 0.85 в trade2stats слишком строгий для fuzzy — отсюда `fuzzy: 0`.

**Альтернативные источники данных:** числовые хэши в GGG trade2 API — это
CRC32 от internal stat names из файлов игры. GGPK/bundle pipeline даст
прямой маппинг hash → stat name без text similarity. `poe-dat-viewer` или
`juddisjudd/ggpk-explorer` (уже упомянут в предыдущем отчёте) — правильное
направление для PoE2 Stats.dat.

---

## 3. Качество данных vs производительность

**`first_seen_at`, `last_seen_at`, `seen_count` — не dead code.** `UpsertListing`
в `repository.go:59–75` корректно их обновляет через `ON CONFLICT`. Данные
реально пишутся.

**Survivorship bias явно не обработан.** В `runner.go:175–196` парсер без
условий обходит все batches из search result. Нет early-stop по дубликатам.
Если редкий топ-предмет выставлен и продан за 30 секунд — он попадёт в БД
только если цикл опроса попал в это окно. Для v1 приемлемо, но надо понимать
что `seen_count` у таких предметов будет 1.

**`currency_rates` растёт без очистки.** Каждый 10-минутный sync вставляет
~8 строк. ~1150 строк в день. Cleanup policy отсутствует.

---

## 4. Подготовка к прайс-движку

**`price_divine` есть и заполняется.** Проверено в `repository.go:54` +
`migrations/000005`. ✓

**`currency_rates` со временной осью есть.** `resolvePriceDivine` берёт курс
не позже `indexed_at` листинга — курс на момент выставления, а не сохранения.
✓

**Mod-set similarity query не готова.** В `market.item_mods` нет denormalized
массива mod_id на уровне item. Запрос «все предметы с Jaccard ≥ 0.6 по
модам» потребует full intersection + outer count через subquery. Работает
через B-tree индекс `idx_market_item_mods_mod_id` при небольшом датасете.
При 100K+ записей нужен GIN на `mod_ids TEXT[]` колонке в `market.items`
(колонки сейчас нет).

**Критичная проблема: `replaceItemMods` в `repository.go:146–163` уничтожает
match-результаты при каждом upsert.**

```go
// repository.go:147
tx.ExecContext(ctx, `DELETE FROM market.item_mods WHERE item_id=$1`, itemID)
```

При каждом обновлении листинга все `mod_id`, `match_status`, `roll_pcts`,
`is_confident` удаляются и вставляются заново как unmatched. Модмэтчер нужно
запускать после каждого цикла парсера иначе вся БД деградирует в
`match_status='unmatched'`. Молчаливая временная зависимость, нигде не
задокументированная.

---

## 5. Что бы делал иначе с нуля

**Материализовал бы хэши в `item_mods` при ingest, а не в matcher.** Сейчас
`extended.hashes` читается из payload уже в matcher при каждом запуске
modmatch. Правильнее: mapper при ingestion записывает `stat_hash` в
`item_mods` сразу — тогда matcher вообще не знает о структуре payload.

**Не строил бы маппинг hash→stat через text similarity.** Числовой хэш —
это детерминированная функция от internal stat name. Есть прямой маппинг
через GGPK/bundle. Text similarity — обходной путь с принципиальным
потолком точности. С нуля — начал бы с GGPK pipeline сразу.

**Сохранял бы хэш мод-секции в `market.items`** чтобы `replaceItemMods`
мог пропускать re-insert при неизменных модах. Одна колонка `mods_hash TEXT`
— и matcher-результаты становятся долгоживущими без внешней координации.

---

## Приоритет на 2 часа

**Исправить `replaceItemMods`: добавить сравнение хэша мод-секции payload
до delete.**

Конкретно: добавить в `market.items` колонку `mods_hash TEXT`, вычислять
SHA256 от сортированного списка `mod_type+line_text+roll_values` при
ingestion, и пропускать `DELETE + re-insert` если хэш совпадает. Это
~часовая правка: миграция + ~10 строк в `upsertItem` + условие в
`UpsertListing`. После этого match-результаты станут стабильными, и
modmatch нужно будет запускать только для новых состояний предметов.
