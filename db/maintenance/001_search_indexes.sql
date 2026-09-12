-- Sky-History search performance: Phase 2 indexes
--
-- Run this MANUALLY, e.g.:
--   psql -h buntbox.andylocal.net -U skyhistory -d skyhistory -f phase2_indexes.sql
--
-- Do NOT put this in processor/db/migrations/. That runner wraps each file in a
-- transaction (processor/db/connect.go:84) and CREATE INDEX CONCURRENTLY cannot
-- run inside one. CONCURRENTLY also lets the processor keep writing during the
-- build, which a plain CREATE INDEX would block for several minutes.

-- Speeds up the build itself; session-local, does not persist.
SET maintenance_work_mem = '1GB';

-- Prefix search fallback ("RYR" -> RYR1AB). The database collation is
-- en_US.utf8, under which a plain btree CANNOT serve LIKE 'X%' -- verified by
-- measurement: plain LIKE still seq-scanned in 711ms. text_pattern_ops is
-- what makes the prefix range scannable. Expect ~320MB and a few minutes.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_flights_callsign_prefix
    ON flights (callsign text_pattern_ops);

-- Registration lookup runs on every callsign-shaped search before falling
-- through, and is currently a seq scan of all 300k aircraft rows.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_aircraft_registration
    ON aircraft (registration);

-- Used by SearchByType's count, which joins flights to aircraft on type_code.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_aircraft_type_code
    ON aircraft (type_code);

-- Two rows have the literal string 'n/a' as their registration. They are the
-- only non-uppercase registrations in the table and are junk, not real marks.
UPDATE aircraft SET registration = NULL WHERE registration = 'n/a';

-- Keep statistics fresh automatically. At the default 10% scale factor a
-- 35M-row table is only analyzed every ~26 days, and that interval grows with
-- the table. 1% is roughly every 350k rows, about 2.5 days at current volume.
ALTER TABLE flights SET (
    autovacuum_analyze_scale_factor = 0.01,
    autovacuum_vacuum_scale_factor  = 0.02
);

ANALYZE flights;
ANALYZE aircraft;

-- Verify the indexes are live and being chosen.
EXPLAIN (ANALYZE, BUFFERS)
SELECT COUNT(*) FROM flights f WHERE f.callsign LIKE 'KLM%';
