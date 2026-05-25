CREATE SCHEMA IF NOT EXISTS market;

CREATE TABLE IF NOT EXISTS market.leagues (
    id BIGSERIAL PRIMARY KEY,
    slug TEXT NOT NULL UNIQUE,
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS market.search_runs (
    id BIGSERIAL PRIMARY KEY,
    league_id BIGINT NOT NULL REFERENCES market.leagues(id),
    query_id TEXT NOT NULL,
    query_hash TEXT NOT NULL,
    requested_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    response_payload JSONB NOT NULL DEFAULT '{}'::jsonb
);

CREATE TABLE IF NOT EXISTS market.items (
    id BIGSERIAL PRIMARY KEY,
    trade_item_id TEXT NOT NULL UNIQUE,
    ref_base_item_id TEXT REFERENCES ref.base_items(id),
    rarity TEXT,
    item_level INT,
    identified BOOLEAN,
    corrupted BOOLEAN,
    mirrored BOOLEAN,
    sockets JSONB NOT NULL DEFAULT '[]'::jsonb,
    properties JSONB NOT NULL DEFAULT '{}'::jsonb,
    requirements JSONB NOT NULL DEFAULT '{}'::jsonb,
    payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS market.item_mods (
    item_id BIGINT NOT NULL REFERENCES market.items(id) ON DELETE CASCADE,
    mod_id TEXT REFERENCES ref.mods(id),
    mod_type TEXT NOT NULL,
    line_text TEXT NOT NULL,
    roll_values JSONB NOT NULL DEFAULT '[]'::jsonb,
    sort_order INT NOT NULL DEFAULT 0,
    PRIMARY KEY (item_id, mod_type, sort_order)
);

CREATE TABLE IF NOT EXISTS market.listings (
    id BIGSERIAL PRIMARY KEY,
    trade_listing_id TEXT NOT NULL UNIQUE,
    item_id BIGINT NOT NULL REFERENCES market.items(id) ON DELETE CASCADE,
    league_id BIGINT NOT NULL REFERENCES market.leagues(id),
    account_name TEXT,
    price_currency TEXT,
    price_amount NUMERIC(20,6),
    indexed_at TIMESTAMPTZ,
    whisper TEXT,
    stash_name TEXT,
    position_x INT,
    position_y INT,
    payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    seen_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_market_search_runs_league_requested ON market.search_runs(league_id, requested_at DESC);
CREATE INDEX IF NOT EXISTS idx_market_items_ref_base ON market.items(ref_base_item_id);
CREATE INDEX IF NOT EXISTS idx_market_item_mods_mod_id ON market.item_mods(mod_id);
CREATE INDEX IF NOT EXISTS idx_market_listings_item_id ON market.listings(item_id);
CREATE INDEX IF NOT EXISTS idx_market_listings_league_seen ON market.listings(league_id, seen_at DESC);
CREATE INDEX IF NOT EXISTS idx_market_listings_price ON market.listings(price_currency, price_amount);
