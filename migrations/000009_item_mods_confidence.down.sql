DROP INDEX IF EXISTS idx_market_item_mods_is_confident;

ALTER TABLE market.item_mods
    DROP COLUMN IF EXISTS is_confident;

