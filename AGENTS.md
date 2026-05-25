# AGENTS.md — PoE2PriceChecker

Локальные правила проекта для агентной работы.

## Цель проекта
Собирать и нормализовывать рыночные данные PoE2 (предметы + цены), чтобы оценивать стоимость по модам и их сочетаниям.

## Архитектурный принцип
- Справочники (базы, классы, теги, моды, переводы статов) не добываем вручную.
- Источник справочников: внешний датасет (в первую очередь RePoE-fork).
- Наша БД хранит только рыночные экземпляры предметов и ссылки на справочник по id.

## Источники данных (приоритет)
1. **Primary:** https://repoe-fork.github.io/poe2/
2. **Cross-check:** https://github.com/SilkroadLabs/rePoE2
3. **Cross-check:** https://github.com/LocalIdentity/poe2-data
4. **Unique items:** Path of Building export / связанные репозитории
5. **Manual UI reference only:** poe2db.tw (без официального API)

## Минимальный набор справочников
- `base_items.json`
- `item_classes.json`
- `tags.json`
- `mods.json`
- `mods_by_base.json`
- `stat_translations.json`
- (по мере надобности) `skill_gems.json`, `gem_tags.json`, `essences.json`, `augments.json`

## Технические договоренности
- Язык: Go
- БД: PostgreSQL
- Парсер — отдельный бинарник (`cmd/parser`)
- Воркеры: поштучная конфигурация, per-worker proxy (none/socks5/https)

## Что не делаем
- Не парсим html-страницы poe2db как основной источник.
- Не храним справочники как «истину» вручную в коде.
- Не теряем ссылочность: instance-данные должны ссылаться на справочник по id.

## Перед изменением схемы
- Проверить, что поле уже есть в upstream датасете и как оно называется.
- Если поле только в рыночном API и нет в датасете — хранить как instance attribute.
