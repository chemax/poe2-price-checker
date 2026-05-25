ALTER TABLE market.search_runs
    ADD COLUMN IF NOT EXISTS query_name TEXT NOT NULL DEFAULT 'default';

CREATE INDEX IF NOT EXISTS idx_market_search_runs_query_name_requested
    ON market.search_runs(query_name, requested_at DESC);
