-- applies-to: postgres,sqlite
-- Drop six log-table indexes that pg_stat_user_indexes showed ZERO scans in
-- eight production days (2026-08-28 .. 2026-09-05). They cost 15 MB on a
-- 124k-row table and are pure write tax at ~12k inserts/day.
--
-- Safe to re-run (IF EXISTS). If an index is later found useful, add it back
-- with CREATE INDEX CONCURRENTLY (PostgreSQL) in a NEW numbered file — never
-- by resurrecting this one.

DROP INDEX IF EXISTS idx_logs_request_id;
DROP INDEX IF EXISTS idx_logs_upstream_request_id;
DROP INDEX IF EXISTS idx_logs_ip;
DROP INDEX IF EXISTS idx_logs_group;
DROP INDEX IF EXISTS idx_logs_token_name;
DROP INDEX IF EXISTS idx_logs_token_id;
