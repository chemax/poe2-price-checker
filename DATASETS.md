# DATASETS.md — внешние справочники PoE2

## Основной источник
- RePoE-fork PoE2 export: https://repoe-fork.github.io/poe2/
- Repo: https://github.com/repoe-fork/repoe-fork.github.io

## Обязательные файлы v1
- `base_items.json`
- `item_classes.json`
- `tags.json`
- `mods.json`
- `mods_by_base.json`
- `stat_translations.json`

## Дополнительные (по фичам)
- `skill_gems.json`
- `gem_tags.json`
- `essences.json`
- `augments.json`

## Альтернативы для сверки
- https://github.com/SilkroadLabs/rePoE2
- https://github.com/LocalIdentity/poe2-data

## Уникальные предметы
- Источник: Path of Building data export (ручная поддержка)

## Политика хранения
- Справочники = external reference dataset
- Наша operational DB = instance/item listing data + price history + FK/reference links
