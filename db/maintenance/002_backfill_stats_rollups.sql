-- Backfill (or rebuild) the statistics rollup tables from the flights table.
--
--   psql -h <db-host> -U skyhistory -d skyhistory -f db/maintenance/002_backfill_stats_rollups.sql
--
-- No reprocessing of releases is needed: every figure here is derived from
-- flights, which is already populated. Takes roughly a minute on ~35M rows.
--
-- Safe to re-run at any time. Because it recomputes from flights it is also
-- the way to repair rollups after aircraft type codes are corrected, which the
-- incremental per-release refresh does not retroactively apply.
--
-- Requires migration 004_stats_rollups.sql to have been applied first.

SET maintenance_work_mem = '1GB';
SET work_mem = '256MB';

\timing on

-- Per-day flight and distinct-aircraft counts.
INSERT INTO daily_stats (date, flight_count, aircraft_count, updated_at)
SELECT date, COUNT(*), COUNT(DISTINCT icao), NOW()
FROM flights
GROUP BY date
ON CONFLICT (date) DO UPDATE
   SET flight_count   = EXCLUDED.flight_count,
       aircraft_count = EXCLUDED.aircraft_count,
       updated_at     = NOW();

-- Per-day, per-type flight counts. LEFT JOIN so a flight whose aircraft row is
-- somehow missing still lands under 'Unknown' rather than disappearing.
TRUNCATE daily_type_stats;
INSERT INTO daily_type_stats (date, type_code, flight_count, description)
SELECT f.date, COALESCE(NULLIF(a.type_code, ''), 'Unknown'), COUNT(*),
       MAX(NULLIF(a.description, ''))
FROM flights f
LEFT JOIN aircraft a ON a.icao = f.icao
GROUP BY 1, 2;

-- Distinct aircraft per period. Days come free from daily_stats; the longer
-- periods are computed with a hash DISTINCT over (period, icao), which
-- measured faster than COUNT(DISTINCT) or one query per period.
INSERT INTO period_aircraft (period_type, period_start, aircraft_count, updated_at)
SELECT 'day', date, aircraft_count, NOW() FROM daily_stats
ON CONFLICT (period_type, period_start) DO UPDATE
   SET aircraft_count = EXCLUDED.aircraft_count, updated_at = NOW();

INSERT INTO period_aircraft (period_type, period_start, aircraft_count, updated_at)
SELECT 'week', period_start, COUNT(*), NOW()
FROM (SELECT DISTINCT date_trunc('week', date)::date AS period_start, icao FROM flights) t
GROUP BY period_start
ON CONFLICT (period_type, period_start) DO UPDATE
   SET aircraft_count = EXCLUDED.aircraft_count, updated_at = NOW();

INSERT INTO period_aircraft (period_type, period_start, aircraft_count, updated_at)
SELECT 'month', period_start, COUNT(*), NOW()
FROM (SELECT DISTINCT date_trunc('month', date)::date AS period_start, icao FROM flights) t
GROUP BY period_start
ON CONFLICT (period_type, period_start) DO UPDATE
   SET aircraft_count = EXCLUDED.aircraft_count, updated_at = NOW();

INSERT INTO period_aircraft (period_type, period_start, aircraft_count, updated_at)
SELECT 'year', period_start, COUNT(*), NOW()
FROM (SELECT DISTINCT date_trunc('year', date)::date AS period_start, icao FROM flights) t
GROUP BY period_start
ON CONFLICT (period_type, period_start) DO UPDATE
   SET aircraft_count = EXCLUDED.aircraft_count, updated_at = NOW();

ANALYZE daily_stats;
ANALYZE daily_type_stats;
ANALYZE period_aircraft;

SELECT 'daily_stats' t, COUNT(*) FROM daily_stats
UNION ALL SELECT 'daily_type_stats', COUNT(*) FROM daily_type_stats
UNION ALL SELECT 'period_aircraft', COUNT(*) FROM period_aircraft;
