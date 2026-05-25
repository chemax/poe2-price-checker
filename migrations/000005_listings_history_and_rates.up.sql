CREATE TABLE IF NOT EXISTS market.currency_rates (
    id BIGSERIAL PRIMARY KEY,
    currency TEXT NOT NULL,
    divine_value NUMERIC(20,8) NOT NULL,
    fetched_at TIMESTAMPTZ NOT NULL,
    source TEXT NOT NULL DEFAULT 'poe.ninja'
);

CREATE INDEX IF NOT EXISTS idx_market_currency_rates_currency_fetched
    ON market.currency_rates(currency, fetched_at DESC);

ALTER TABLE market.listings
    ADD COLUMN IF NOT EXISTS price_divine NUMERIC(20,8),
    ADD COLUMN IF NOT EXISTS first_seen_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    ADD COLUMN IF NOT EXISTS last_seen_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    ADD COLUMN IF NOT EXISTS seen_count INT NOT NULL DEFAULT 1;

UPDATE market.listings
SET first_seen_at = COALESCE(first_seen_at, seen_at),
    last_seen_at = COALESCE(last_seen_at, seen_at),
    seen_count = CASE WHEN seen_count IS NULL OR seen_count < 1 THEN 1 ELSE seen_count END;

CREATE INDEX IF NOT EXISTS idx_market_listings_indexed_at
    ON market.listings(indexed_at DESC);

CREATE INDEX IF NOT EXISTS idx_market_item_mods_item_type
    ON market.item_mods(item_id, mod_type);
