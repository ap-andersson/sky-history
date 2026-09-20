# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

Live gap-fill: the days the archive has not published yet are searchable from
crowdsourced ADS-B receivers.

### Added

- **`ENABLE_LIVE_GAPFILL`**, off by default and required on both the `api` and
  `collector` services. Until it is set the collector polls nothing, the
  `/api/feeders` routes are never registered, `include_live` is ignored, and
  neither the Feeders page nor its setting appears in the UI — so updating an
  existing deployment does not start accepting public submissions by surprise.
  `GET /api/config` reports which optional features are on.
- **`shared/` Go module**, required by `processor`, `api` and `collector`
  through a relative `replace` directive. Their Docker build contexts are now
  the repository root (`docker build -f api/Dockerfile .`) so the sibling
  module is reachable; a `.dockerignore` keeps that context small. It holds
  `feedcheck`, the `.env` discovery and typed environment readers that were
  copied into all three services, and the `Aircraft` and `Flight` domain rows.
  What stays per-service is what is genuinely per-service: the `Config` structs
  (three unrelated sets of knobs), the parser's `ParsedAircraft`, and the API's
  response shapes.
- **`collector` service.** Polls approved feeders for readsb's `aircraft.json`
  and writes provisional flight rows covering the window between the last
  processed release and now. Sightings from every feeder fold into one segment
  per aircraft, so more feeders widen coverage rather than duplicating rows.
  Open segments are written on each flush, so an aircraft still in the air is
  searchable with a `last_seen` that advances.
- **`feeders` table and submit form.** Feeders live in the database rather than
  in configuration, and anyone can offer one through the new **Feeders** page.
  A submission is fetched and checked against the `aircraft.json` shape
  immediately, then recorded with `enabled = FALSE` until an administrator
  flips it by hand. The collector re-reads the roster on an interval, so
  approving a feeder needs no restart. The public roster shows names and status
  only; submitted URLs stay in the database.
- **`shared/feedcheck`**, used by both the API and the collector, which guards
  every fetch of a submitted URL: scheme allowlist, the vetted IP dialled directly to
  close the DNS-rebinding window, refusal of loopback, private, link-local,
  CGNAT, multicast, reserved and NAT64 ranges, a response size cap, and error
  messages that do not reveal what is listening on an internal address.
  Submissions are rate limited per address, in memory and over 24 hours.
- **`flights.source`** (`archive` or `live`) and **`flights.feeder_id`**, so
  live rows are distinguishable in results and one feeder's output can be
  purged on its own.
- **Settings → Live gap-fill data**, off by default. Live rows are excluded
  from search unless asked for, via `include_live=true` on the search
  endpoints, and are badged where they appear.

### Removed

- `processor/models`' unused `Flight` and `ProcessedRelease` types, which
  nothing referenced.

### Changed

- The processor deletes a date's live rows inside the transaction that ingests
  its release, so the archive replaces them wholesale. They are never merged:
  the archive segments flights on the trace's new-leg flag and the collector on
  a gap timeout, so the same flight lands on boundaries no unique constraint
  would recognise as a duplicate.
- The collector refuses to write any row for a date that already has a
  processed release, and the processor sweeps stragglers each poll cycle. Both
  are needed: a segment open across the moment a release lands would otherwise
  be rewritten after every sweep.
- Statistics count archive data only. Flight rollups filter on `source`, and
  aircraft totals use the new `aircraft.archive_seen` flag, which the processor
  sets and the collector never does. The collector still writes `aircraft` rows
  — a live flight needs a registration and a type — but an airframe seen only
  in the gap window counts towards nothing until a release confirms it. The
  week, month and year spans reach forward into the days the collector is still
  writing, so without this the numbers would move as the gap fills and unfills.

## [1.3.0] - 2026-09-13

Searching by aircraft type no longer depends on how common the type is.

### Added

- `flights.aircraft_type_id`, with `idx_flights_type_date
  (aircraft_type_id, date DESC, first_seen DESC)`, so a search by type is
  answered by one index already in sort order.
- `db/maintenance/003_backfill_flight_type.sql` to populate the column for
  existing rows, and to create the supporting indexes.
- The processor fills in the type for flights that were recorded before their
  aircraft had been identified, on each release.

### Changed

- Searching by aircraft type no longer joins `flights` to `aircraft`.
  PostgreSQL cannot estimate how many flights a type has when the correlation
  spans two tables, so it walked the date index hunting for matches — fine for
  a common type, catastrophic for a rare one. Results are unchanged, verified
  against the previous implementation across eight types on both search
  endpoints.

  | Type | Flights   | Before  | After |
  |------|-----------|---------|-------|
  | SB39 | 133       | 5,540ms | 9ms   |
  | B742 | 819       | 1,580ms | 8ms   |
  | A306 | 58,266    | 280ms   | 13ms  |
  | A320 | 3,109,890 | 510ms   | 258ms |

  Fetching rows now costs ~14ms for every type; `A320` and `B738` are dominated
  by their exact `COUNT(*)`, not by the search.

- When an aircraft's type changes, its earlier flights keep the type recorded at
  the time; only flights with no type at all are filled in later. An ICAO hex
  can be reassigned to a different airframe, and last year's flights should not
  retroactively become the new aircraft's type.

### Upgrade notes

**Run `db/maintenance/003_backfill_flight_type.sql` after upgrading.** Until it
has run, `flights.aircraft_type_id` is NULL for every existing row and searching
by aircraft type returns **no results** rather than an error. Migration 005 only
adds the column; the processor fills it in for new flights from then on.

Budget a couple of hours on a 35M-row table. Every row is rewritten and each
rewrite updates every index on `flights`, so the table and its indexes grow
substantially during the process — roughly 10GB from 7GB in our case. The script
works one month at a time with a vacuum between, which keeps the peak bounded
and lets the processor keep running throughout. Afterwards,
`REINDEX TABLE CONCURRENTLY flights` reclaims the churn; it took under three
minutes and returned more space than the backfill consumed.

If PostgreSQL runs in Docker, make sure the container has `shm_size: 1gb` (see
the Compose example). With the default 64MB, parallel maintenance operations
fail with `could not resize shared memory segment ... No space left on device`.

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

[Unreleased]: https://github.com/ap-andersson/sky-history/compare/v1.3.0...HEAD
[1.3.0]: https://github.com/ap-andersson/sky-history/releases/tag/v1.3.0
[1.2.0]: https://github.com/ap-andersson/sky-history/releases/tag/v1.2.0
[1.1.0]: https://github.com/ap-andersson/sky-history/releases/tag/v1.1.0
