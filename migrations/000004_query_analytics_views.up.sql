CREATE OR REPLACE VIEW market.v_query_runs AS
SELECT
    sr.id,
    l.slug AS league,
    sr.query_name,
    sr.query_id,
    sr.query_hash,
    sr.requested_at,
    COALESCE(jsonb_array_length(sr.response_payload->'result'), 0) AS result_count
FROM market.search_runs sr
JOIN market.leagues l ON l.id = sr.league_id;

CREATE OR REPLACE VIEW market.v_query_stats_24h AS
SELECT
    league,
    query_name,
    COUNT(*) AS runs,
    AVG(result_count)::numeric(10,2) AS avg_result_count,
    MAX(result_count) AS max_result_count,
    SUM(CASE WHEN result_count > 0 THEN 1 ELSE 0 END)::numeric / NULLIF(COUNT(*), 0) AS hit_rate,
    MIN(requested_at) AS first_seen,
    MAX(requested_at) AS last_seen
FROM market.v_query_runs
WHERE requested_at >= NOW() - INTERVAL '24 hours'
GROUP BY league, query_name;
