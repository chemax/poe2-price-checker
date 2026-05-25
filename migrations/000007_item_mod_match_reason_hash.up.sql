ALTER TABLE market.item_mods
    ADD COLUMN IF NOT EXISTS stat_hash TEXT,
    ADD COLUMN IF NOT EXISTS match_reason TEXT;

CREATE INDEX IF NOT EXISTS idx_market_item_mods_match_reason
    ON market.item_mods(match_reason);
