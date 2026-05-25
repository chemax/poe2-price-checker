DROP INDEX IF EXISTS idx_market_item_mods_item_type;
DROP INDEX IF EXISTS idx_market_listings_indexed_at;

ALTER TABLE market.listings
    DROP COLUMN IF EXISTS seen_count,
    DROP COLUMN IF EXISTS last_seen_at,
    DROP COLUMN IF EXISTS first_seen_at,
    DROP COLUMN IF EXISTS price_divine;

DROP INDEX IF EXISTS idx_market_currency_rates_currency_fetched;
DROP TABLE IF EXISTS market.currency_rates;
