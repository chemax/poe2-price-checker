ALTER TABLE market.item_mods
    ADD COLUMN IF NOT EXISTS is_confident BOOLEAN NOT NULL DEFAULT FALSE;

CREATE INDEX IF NOT EXISTS idx_market_item_mods_is_confident
    ON market.item_mods(is_confident);

