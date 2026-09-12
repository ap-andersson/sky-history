# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

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
