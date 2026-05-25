CREATE TABLE IF NOT EXISTS ref.trade2_stats (
    hash_id TEXT PRIMARY KEY,
    stat_type TEXT NOT NULL,
    trade_text TEXT NOT NULL,
    normalized_text TEXT NOT NULL,
    stat_id TEXT,
    map_method TEXT,
    source_ts TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_ref_trade2_stats_stat_id ON ref.trade2_stats(stat_id);
CREATE INDEX IF NOT EXISTS idx_ref_trade2_stats_stat_type ON ref.trade2_stats(stat_type);
CREATE INDEX IF NOT EXISTS idx_ref_trade2_stats_map_method ON ref.trade2_stats(map_method);
