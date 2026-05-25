DROP INDEX IF EXISTS idx_market_search_runs_query_name_requested;
ALTER TABLE market.search_runs DROP COLUMN IF EXISTS query_name;
