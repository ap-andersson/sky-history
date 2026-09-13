# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [1.2.0] - 2026-09-13

Statistics are now pre-aggregated instead of computed per request, and the UI
gains a menu, a Data page and display settings.

### Added

- Pre-aggregated statistics tables (`daily_stats`, `daily_type_stats`,
  `period_aircraft`), refreshed by the processor inside the same transaction
  that inserts a release's flights, so they can never disagree with the data
  they summarise. Figures are recomputed from `flights` rather than accumulated
  from parse counters, making a reprocess idempotent. Adds ~2.6s to a release.
- `db/maintenance/002_backfill_stats_rollups.sql` to populate the rollups from
  existing data, or rebuild them later. **No reprocessing of releases is
  required** — everything is derived from `flights`. Takes ~1 minute on 35M rows.
- A main menu — Start, Data, Statistics, Settings — replacing the processing
  stats bar that sat above every page next to a Statistics link.
- A **Data** page carrying the figures from that bar, plus the releases that
  failed to parse. Failures were previously behind a small warning button that
  truncated each error to one line; they now show the release tag, attempt count
  and the full message.
- A **Settings** page, stored in the browser via `localStorage`:
  - Time zone: UTC (default) or the browser's zone.
  - Clock: 24-hour (default) or 12-hour.
  - Date format: year-first ISO (default), day-first with slashes or dots, or
    month-first.

### Changed

- The statistics endpoints read the rollup tables instead of aggregating over
  `flights`, so cost is proportional to the length of the period rather than the
  number of flights in it. Output is unchanged, verified field by field against
  the previous implementation across all four periods, including every per-type
  count and description.
  - `/api/stats/period?period=year` 13.3s to 48ms; month 2.7s to 23ms;
    week 1.7s to 22ms; day 1.0s to 17ms.
  - `/api/stats` 670ms to 26ms. It was running `COUNT(*)` over all 35M flight
    rows on every page load; the total now comes from `daily_stats`, which sums
    to exactly the same number. The period endpoint was paying that cost too,
    purely to look up the newest processed date.
  - Unique aircraft per period is precomputed, being the one figure that cannot
    be summed from daily rows: an aircraft flying on 100 days counts once for
    the year.
  - Per-type descriptions come from a per-day `MAX` stored in the rollup. `MAX`
    decomposes over partitions, so aggregating across a range reproduces exactly
    what the previous join to `aircraft` produced.
- Quick search and Filters are a toggle showing one at a time. The gear button
  previously revealed the advanced panel below the still-visible quick search
  box, which suggested the two combined; they are separate queries.
- Search appears only where it applies — Start and the result views. Data,
  Statistics and Settings no longer carry a search box unrelated to them.
- Times and dates follow the Settings preferences throughout. The time zone
  setting shifts instants only: a flight keeps the UTC day its release belongs
  to, because that date is what search, paging and the aircraft detail URL are
  keyed on. The zone is never printed in tables, only offered as a tooltip.
- Failed releases stack into blocks below 640px, where four columns holding a
  long identifier and a long error message were unreadable.

### Fixed

- The busiest-day flight count silently reported 0 when its query failed; the
  error was discarded rather than surfaced.

### Upgrade notes

Run `db/maintenance/002_backfill_stats_rollups.sql` after upgrading. Until it is
run, the statistics endpoints report zeroes: migration 004 creates the tables
empty, and the processor only fills in dates it processes from then on.

Upgrade the **processor** as well as the API, not just one. The API only reads
the rollups, but the processor is what keeps them current — an older processor
will write flights without updating the rollups, and the statistics for those
days will quietly under-report until the backfill script is run again.

Display settings are per-browser and need no migration; everything continues to
be stored and queried in UTC.

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

[Unreleased]: https://github.com/ap-andersson/sky-history/compare/v1.2.0...HEAD
[1.2.0]: https://github.com/ap-andersson/sky-history/releases/tag/v1.2.0
[1.1.0]: https://github.com/ap-andersson/sky-history/releases/tag/v1.1.0
