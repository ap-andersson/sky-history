# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- Pre-aggregated statistics tables (`daily_stats`, `daily_type_stats`,
  `period_aircraft`), refreshed by the processor inside the same transaction
  that inserts a release's flights, so they can never disagree with the data
  they summarise. Figures are recomputed from `flights` rather than accumulated
  from parse counters, making a reprocess idempotent. Adds ~2.6s to a release.
- `db/maintenance/002_backfill_stats_rollups.sql` to populate the rollups from
  existing data, or rebuild them later. **No reprocessing of releases is
  required** — everything is derived from `flights`. Takes ~1 minute on 35M rows.

### Changed

- Statistics page aggregation is substantially faster; the yearly view went
  from ~13.3s to ~3.6s. No API response changes -- output was verified
  identical across all four periods, including all 1527 per-type counts.
  - Unique aircraft is counted by probing the `aircraft` table with `EXISTS`
    rather than `COUNT(DISTINCT f.icao)`, which forces a sort over every
    flight row in the period (4085ms to 484ms). The two are equivalent because
    `flights.icao` is a foreign key into `aircraft`.
  - Total flights, the busiest day and the time series now come from a single
    scan grouped by date, returning a few hundred rows that are aggregated in
    Go, instead of four separate scans of the same rows (5610ms to 1205ms).
    This also removes a `TO_CHAR` grouping that defeated cheaper plans.
  - The per-type breakdown collapses flights to one row per aircraft before
    joining, keeping the join at ~300k rows rather than one row per flight.
  - The four independent aggregations run concurrently, so the wall-clock cost
    is the slowest rather than their sum.
- Fixed the busiest-day flight count silently reporting 0 on query failure; its
  error was previously discarded.
- The statistics endpoints now read the rollup tables rather than aggregating
  over `flights`. Output is unchanged, verified field by field against the
  previous implementation across all four periods including every per-type
  count and description.
  - `/api/stats/period?period=year` 13.3s to 48ms; month 2.7s to 23ms;
    week 1.7s to 22ms; day 1.0s to 17ms.
  - `/api/stats` 670ms to 26ms. It was running `COUNT(*)` over all 35M flight
    rows on every page load; the total now comes from `daily_stats`, which sums
    to exactly the same number.
  - The period endpoint no longer computes the full stats summary just to find
    the newest processed date.
  - Per-type descriptions come from a per-day `MAX` stored in the rollup.
    `MAX` decomposes over partitions, so the range aggregate reproduces exactly
    what the previous join to `aircraft` produced.

### Upgrade notes

Run `db/maintenance/002_backfill_stats_rollups.sql` after upgrading. Until it is
run, the statistics endpoints will report zeroes: migration 004 creates the
tables empty, and the processor only fills in dates it processes from then on.

## [1.1.0] - 2026-09-12

Search performance release. Free-text search went from ~8.3 seconds to ~13
milliseconds on a 34.9M-row `flights` table.

### Fixed

- Free-text search no longer scans the entire `flights` table. Every query
  wrapped its column in `UPPER()` or used `ILIKE`, neither of which a B-tree
  index can serve, so each search performed two full scans of a 2.3GB table.
  Both were also redundant: the processor already stores `icao`, `callsign` and
  `type_code` uppercased and trimmed, and every API handler uppercases its input.
- `SearchByCallsign` now tries an exact match first, served directly by
  `idx_flights_callsign`, and falls back to a prefix search only when that
  misses. A search that matches nothing costs one query instead of two.
- Prefix matching uses `LIKE` rather than `ILIKE` so the index is reachable.
  This applies to the advanced search callsign filter as well.

### Added

- `db/maintenance/001_search_indexes.sql` — search indexes applied manually
  rather than through the migration runner, which wraps each file in a
  transaction where `CREATE INDEX CONCURRENTLY` is not permitted:
  - `idx_flights_callsign_prefix` on `flights (callsign text_pattern_ops)`,
    required because the `en_US.utf8` collation prevents a plain B-tree from
    serving `LIKE 'X%'`.
  - `idx_aircraft_registration` and `idx_aircraft_type_code`.
  - Lower autovacuum scale factors on `flights`, so a table this size is
    analyzed roughly every 2.5 days instead of every ~26 days.
- PostgreSQL tuning in the documented Compose example (`shared_buffers`,
  `effective_cache_size`, `work_mem`, `random_page_cost`, `shm_size`).
- **Database Performance** section in the README covering the required indexes,
  why `UPPER()`/`ILIKE` must be avoided, and how to size the memory settings.
- Versioned container images and automated GitHub Releases.

### Changed

- `latest` container images now track the newest tagged release rather than
  every push to `main`. Use `edge` to follow `main`.

### Upgrade notes

Run `db/maintenance/001_search_indexes.sql` against your database after
upgrading; the indexes are not created automatically. The script is idempotent
and builds indexes `CONCURRENTLY`, so ingest continues during the build.

If you pull `latest`, note that it now moves only on releases. Switch to `edge`
to keep following `main`.

## [1.0.0] - Initial

Initial state of the project prior to versioned releases: processor, API,
frontend and PostgreSQL schema, deployed via Docker Compose with images built
from `main`. Never formally tagged; recorded here for continuity.

[Unreleased]: https://github.com/ap-andersson/sky-history/compare/v1.1.0...HEAD
[1.1.0]: https://github.com/ap-andersson/sky-history/releases/tag/v1.1.0
