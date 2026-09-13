-- Pre-aggregated statistics, maintained by the processor after each release.
--
-- The stats page used to aggregate over the whole flights table on every
-- request, which grew linearly with the data. These tables hold the same
-- numbers at a few hundred rows per year.
--
-- Everything here is derived from flights, so it can be rebuilt at any time
-- with db/maintenance/002_backfill_stats_rollups.sql.

-- One row per day. Serves the time series, total flights (SUM) and the
-- busiest day (MAX) for any range.
CREATE TABLE IF NOT EXISTS daily_stats (
    date           DATE PRIMARY KEY,
    flight_count   INTEGER NOT NULL,
    aircraft_count INTEGER NOT NULL,
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- One row per day per aircraft type. 'Unknown' stands in for aircraft with no
-- type code, matching what the API reported before: a NULL cannot sit in a
-- primary key, and the sentinel keeps the output identical.
-- description holds the day's MAX(aircraft.description) for the type. MAX
-- decomposes over partitions, so MAX across a range of days equals the MAX the
-- API previously computed by joining flights to aircraft directly -- the output
-- is identical without the join.
CREATE TABLE IF NOT EXISTS daily_type_stats (
    date         DATE NOT NULL,
    type_code    TEXT NOT NULL,
    flight_count INTEGER NOT NULL,
    description  TEXT,
    PRIMARY KEY (date, type_code)
);

-- Distinct aircraft per period. This is the one figure that cannot be summed
-- from daily_stats: an aircraft flying on 100 days counts once for the year.
-- Only the four period types the API exposes are maintained, and a release
-- only affects the four periods containing its date.
CREATE TABLE IF NOT EXISTS period_aircraft (
    period_type    TEXT NOT NULL CHECK (period_type IN ('day','week','month','year')),
    period_start   DATE NOT NULL,
    aircraft_count INTEGER NOT NULL,
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (period_type, period_start)
);
