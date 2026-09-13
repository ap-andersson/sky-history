-- Populate flights.aircraft_type_id from the aircraft table.
--
--   psql -h <db-host> -U skyhistory -d skyhistory -f db/maintenance/003_backfill_flight_type.sql
--
-- Requires migration 005_flight_type.sql (which adds the column) to have run.
--
-- Denormalising the type onto flights lets a single index serve
-- "flights of type X, most recent first", which a join to aircraft cannot:
-- the planner has no way to estimate how many flights a given type has, and
-- for rare types it walks the whole date index looking for matches.
--
-- Done one month at a time, with a VACUUM between. PostgreSQL implements
-- UPDATE as delete-plus-insert, so updating all 35M rows in one statement
-- would leave a whole table's worth of dead tuples before anything could be
-- reclaimed. Per-month chunks keep the peak at a few hundred MB and let the
-- processor keep writing throughout.
--
-- Safe to re-run: rows already holding the right value are skipped.
--
-- PARALLEL 0 on the vacuums: a parallel vacuum sizes its dead-tuple store from
-- maintenance_work_mem and puts it in shared memory, which under Docker means
-- /dev/shm -- 64MB by default, against the 510MB a 1GB maintenance_work_mem
-- asks for. Rather than depend on the container being configured with a larger
-- shm_size, vacuum single-threaded here; it is not the slow part.

\timing on

UPDATE flights f SET aircraft_type_id = a.aircraft_type_id FROM aircraft a
WHERE a.icao = f.icao AND f.date >= '2025-12-01' AND f.date < '2026-01-01'
  AND f.aircraft_type_id IS DISTINCT FROM a.aircraft_type_id;
VACUUM (ANALYZE, PARALLEL 0) flights;

UPDATE flights f SET aircraft_type_id = a.aircraft_type_id FROM aircraft a
WHERE a.icao = f.icao AND f.date >= '2026-01-01' AND f.date < '2026-02-01'
  AND f.aircraft_type_id IS DISTINCT FROM a.aircraft_type_id;
VACUUM (ANALYZE, PARALLEL 0) flights;

UPDATE flights f SET aircraft_type_id = a.aircraft_type_id FROM aircraft a
WHERE a.icao = f.icao AND f.date >= '2026-02-01' AND f.date < '2026-03-01'
  AND f.aircraft_type_id IS DISTINCT FROM a.aircraft_type_id;
VACUUM (ANALYZE, PARALLEL 0) flights;

UPDATE flights f SET aircraft_type_id = a.aircraft_type_id FROM aircraft a
WHERE a.icao = f.icao AND f.date >= '2026-03-01' AND f.date < '2026-04-01'
  AND f.aircraft_type_id IS DISTINCT FROM a.aircraft_type_id;
VACUUM (ANALYZE, PARALLEL 0) flights;

UPDATE flights f SET aircraft_type_id = a.aircraft_type_id FROM aircraft a
WHERE a.icao = f.icao AND f.date >= '2026-04-01' AND f.date < '2026-05-01'
  AND f.aircraft_type_id IS DISTINCT FROM a.aircraft_type_id;
VACUUM (ANALYZE, PARALLEL 0) flights;

UPDATE flights f SET aircraft_type_id = a.aircraft_type_id FROM aircraft a
WHERE a.icao = f.icao AND f.date >= '2026-05-01' AND f.date < '2026-06-01'
  AND f.aircraft_type_id IS DISTINCT FROM a.aircraft_type_id;
VACUUM (ANALYZE, PARALLEL 0) flights;

UPDATE flights f SET aircraft_type_id = a.aircraft_type_id FROM aircraft a
WHERE a.icao = f.icao AND f.date >= '2026-06-01' AND f.date < '2026-07-01'
  AND f.aircraft_type_id IS DISTINCT FROM a.aircraft_type_id;
VACUUM (ANALYZE, PARALLEL 0) flights;

UPDATE flights f SET aircraft_type_id = a.aircraft_type_id FROM aircraft a
WHERE a.icao = f.icao AND f.date >= '2026-07-01' AND f.date < '2026-08-01'
  AND f.aircraft_type_id IS DISTINCT FROM a.aircraft_type_id;
VACUUM (ANALYZE, PARALLEL 0) flights;

UPDATE flights f SET aircraft_type_id = a.aircraft_type_id FROM aircraft a
WHERE a.icao = f.icao AND f.date >= '2026-08-01' AND f.date < '2026-09-01'
  AND f.aircraft_type_id IS DISTINCT FROM a.aircraft_type_id;
VACUUM (ANALYZE, PARALLEL 0) flights;

UPDATE flights f SET aircraft_type_id = a.aircraft_type_id FROM aircraft a
WHERE a.icao = f.icao AND f.date >= '2026-09-01'
  AND f.aircraft_type_id IS DISTINCT FROM a.aircraft_type_id;
VACUUM (ANALYZE, PARALLEL 0) flights;

SELECT COUNT(*) FILTER (WHERE aircraft_type_id IS NOT NULL) AS typed,
       COUNT(*) FILTER (WHERE aircraft_type_id IS NULL)     AS untyped,
       COUNT(*) AS total
FROM flights;

-- Indexes, created after the backfill so they are built once against final
-- data rather than maintained through 35M row rewrites.
--
-- Run these separately; CREATE INDEX CONCURRENTLY cannot run inside a
-- transaction, and this file is executed statement by statement.

-- Serves "flights of type X, most recent first" directly in sort order, so
-- LIMIT stops after the rows it needs however rare the type is.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_flights_type_date
    ON flights (aircraft_type_id, date DESC, first_seen DESC);

-- The processor fills in types for flights recorded before their aircraft was
-- identified. Without this it would scan all 35M rows on every release to find
-- the few that qualify. Only untyped rows are indexed, so it stays small and
-- shrinks as aircraft get identified.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_flights_untyped
    ON flights (icao) WHERE aircraft_type_id IS NULL;

ANALYZE flights;
