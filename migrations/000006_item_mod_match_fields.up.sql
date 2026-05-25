ALTER TABLE market.item_mods
    ADD COLUMN IF NOT EXISTS match_status TEXT NOT NULL DEFAULT 'unmatched',
    ADD COLUMN IF NOT EXISTS matched_text TEXT,
    ADD COLUMN IF NOT EXISTS roll_pcts JSONB NOT NULL DEFAULT '[]'::jsonb;

CREATE INDEX IF NOT EXISTS idx_market_item_mods_match_status
    ON market.item_mods(match_status);
