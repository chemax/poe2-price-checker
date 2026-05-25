DROP INDEX IF EXISTS idx_market_item_mods_match_status;

ALTER TABLE market.item_mods
    DROP COLUMN IF EXISTS roll_pcts,
    DROP COLUMN IF EXISTS matched_text,
    DROP COLUMN IF EXISTS match_status;
