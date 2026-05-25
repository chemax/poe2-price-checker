CREATE SCHEMA IF NOT EXISTS ref;

CREATE TABLE IF NOT EXISTS ref.datasets (
    id BIGSERIAL PRIMARY KEY,
    source_name TEXT NOT NULL,
    source_url TEXT NOT NULL,
    version TEXT,
    checksum TEXT,
    fetched_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (source_name, version, checksum)
);

CREATE TABLE IF NOT EXISTS ref.item_classes (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    dataset_id BIGINT REFERENCES ref.datasets(id) ON DELETE SET NULL,
    payload JSONB NOT NULL DEFAULT '{}'::jsonb
);

CREATE TABLE IF NOT EXISTS ref.tags (
    id TEXT PRIMARY KEY,
    name TEXT,
    dataset_id BIGINT REFERENCES ref.datasets(id) ON DELETE SET NULL,
    payload JSONB NOT NULL DEFAULT '{}'::jsonb
);

CREATE TABLE IF NOT EXISTS ref.base_items (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    item_class_id TEXT REFERENCES ref.item_classes(id),
    required_level INT,
    width SMALLINT,
    height SMALLINT,
    tags TEXT[] NOT NULL DEFAULT '{}',
    requirements JSONB NOT NULL DEFAULT '{}'::jsonb,
    properties JSONB NOT NULL DEFAULT '{}'::jsonb,
    payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    dataset_id BIGINT REFERENCES ref.datasets(id) ON DELETE SET NULL
);

CREATE TABLE IF NOT EXISTS ref.mods (
    id TEXT PRIMARY KEY,
    domain TEXT,
    generation_type TEXT,
    required_level INT,
    is_essence_only BOOLEAN,
    is_fractured BOOLEAN,
    stats JSONB NOT NULL DEFAULT '[]'::jsonb,
    spawn_weights JSONB NOT NULL DEFAULT '[]'::jsonb,
    tags TEXT[] NOT NULL DEFAULT '{}',
    payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    dataset_id BIGINT REFERENCES ref.datasets(id) ON DELETE SET NULL
);

CREATE TABLE IF NOT EXISTS ref.mods_by_base (
    base_item_id TEXT NOT NULL REFERENCES ref.base_items(id) ON DELETE CASCADE,
    mod_id TEXT NOT NULL REFERENCES ref.mods(id) ON DELETE CASCADE,
    dataset_id BIGINT REFERENCES ref.datasets(id) ON DELETE SET NULL,
    PRIMARY KEY (base_item_id, mod_id)
);

CREATE TABLE IF NOT EXISTS ref.stat_translations (
    stat_id TEXT PRIMARY KEY,
    english_text TEXT,
    payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    dataset_id BIGINT REFERENCES ref.datasets(id) ON DELETE SET NULL
);

CREATE INDEX IF NOT EXISTS idx_ref_base_items_item_class_id ON ref.base_items(item_class_id);
CREATE INDEX IF NOT EXISTS idx_ref_base_items_tags_gin ON ref.base_items USING GIN(tags);
CREATE INDEX IF NOT EXISTS idx_ref_mods_tags_gin ON ref.mods USING GIN(tags);
CREATE INDEX IF NOT EXISTS idx_ref_mods_generation_type ON ref.mods(generation_type);
CREATE INDEX IF NOT EXISTS idx_ref_mods_required_level ON ref.mods(required_level);
