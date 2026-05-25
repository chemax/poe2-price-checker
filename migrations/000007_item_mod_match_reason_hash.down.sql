DROP INDEX IF EXISTS idx_market_item_mods_match_reason;

ALTER TABLE market.item_mods
    DROP COLUMN IF EXISTS match_reason,
    DROP COLUMN IF EXISTS stat_hash;
