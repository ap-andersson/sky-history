# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- **Custom HTML hooks.** Mount a folder at `/etc/sky-history/custom` in the
  frontend container and nginx inserts its `head.html` at the end of `<head>`
  and `body-end.html` at the end of `<body>` on every page -- for analytics
  such as a self-hosted Plausible, verification tags and the like -- without
  rebuilding the image. Missing files insert nothing, so existing deployments
  are unchanged.

## [1.7.0] - 2026-09-20

A public, read-only API for search and stats.

### Added

- **`/api/public/search`** and **`/api/public/stats`**, registered only when
  `ENABLE_PUBLIC_API=true`. Thin wrappers around the same handlers the
  frontend already calls internally, so a public response is byte for byte
  what the internal one is -- no separate schema to keep in sync as the app's
  own response shapes change.
- **`api_keys` table**, minted with two SQL statements (`pgcrypto`'s
  `gen_random_bytes` + `digest`) rather than any admin UI or CLI -- the same
  "flip a column by hand" pattern feeders already use for approval. Only a
  hash of the key is ever stored. `last_used_at` and `request_count` update on
  every authorized request.
- **Two rate-limit layers.** A per-key limit (`PUBLIC_API_RATE_LIMIT_PER_MINUTE`,
  default 120, shared across both endpoints, no per-key override) returns
  `429` with `X-RateLimit-Limit`/`X-RateLimit-Remaining` headers on every
  response. A separate, tighter, per-address guard (20/min, not configurable)
  sits ahead of key validation specifically on the missing-key and
  wrong-key paths, since a guessed key still costs a database lookup and
  would otherwise let an attacker spam guesses for free.
- **`api/handlers/ratelimit.go`**: a `slidingWindowLimiter` extracted from the
  feeder submission form's existing burst guard rather than writing a second
  copy for the public API. The feeder handler now uses it too.
- **An "API" page**, shown once the feature is enabled: what's available, the
  rate limit (read live from `/api/config` rather than hardcoded, so it can
  never drift from what is actually deployed), and how to request a key by
  emailing skyhistory@andymail.net. Full parameter documentation stays in the
  README rather than being duplicated in the UI.
- **A `## Public API` README section**: minting and revoking a key, both
  rate-limit layers explained (including what nginx's own `limit_req` is
  actually limiting on -- source IP, not key, since nginx never sees one --
  and why it needs *more* headroom than the per-key limit, not less), and an
  optional nginx snippet for a coarse outer guard in front of the real one.

### Fixed

- The per-address guard above was, until now, checked on *every* request
  regardless of whether it carried a valid key -- so a single legitimate
  integration could never exceed 20/min no matter what
  `PUBLIC_API_RATE_LIMIT_PER_MINUTE` was set to. It now only applies on the
  missing-key and wrong-key paths; a valid key's traffic goes straight to the
  per-key limiter untouched. Caught by asking why an outer per-source limit
  would ever need to be *lower* than the per-key limit it sits in front of --
  it shouldn't, and the code did not actually match what the docs already
  described.
- `h2` (every page's own title) was set to `var(--text-dim)` while `h3`
  (its subsections) inherited full brightness, so a page's title read dimmer
  than its own subheadings. Removed the override.
- `.failed-table-wrap` (the Data page's error table, and the API page's
  endpoint table) had no margin at all, so whatever followed sat flush
  against it.

## [1.6.0] - 2026-09-20

A photo of the aircraft on its detail page.

### Added

- **An aircraft photo** on `/aircraft/:icao`, fetched from
  [planespotters.net](https://www.planespotters.net) by ICAO hex (refined with
  registration and type once known) the same way tar1090 does it when you
  select an aircraft on the map: a plain client-side `fetch`, no API key, no
  backend involved. Every photo credits its photographer and links back to its
  page on planespotters.net, opened in a new tab, per their photo API terms —
  see the new note under [Data Source](#data-source). Resolves to no photo
  shown, silently, whenever none is found or the lookup fails; this is a
  nice-to-have next to the archive data, not something worth an error state
  for. Cached in memory for the life of the tab, since the photo cannot change
  while flipping between dates for the same aircraft.

### Changed

- The photo sits beside the aircraft card's date picker and external links as
  a sibling of that whole block (`.aircraft-card-body`), not inside the
  single-line header row next to the ICAO text. It first shipped inside that
  row, where a photo taller than one line of text stretched the row and
  pushed the date picker and links down with it — and on narrow screens
  wedged itself between the ICAO line and the date picker rather than
  wrapping to the end of the block. Sitting beside the whole column instead
  means the photo's height can only ever change the card's total height, never
  the spacing between the controls inside it.

## [1.5.0] - 2026-09-20

A light theme, and a real logo in place of the ✈ placeholder.

### Added

- **Appearance setting**: Dark, Light, or Match system, under **Settings**.
  Dark stays the default, so an existing deployment looks unchanged after
  upgrading. Every color in `style.css` runs through a small set of CSS
  variables (`--bg`, `--surface`, `--accent`, `--accent-rgb`, and so on), so
  the light palette is a second token block rather than a parallel stylesheet.
  Match system tracks `prefers-color-scheme` live via a media-query listener,
  so an already-open tab follows the OS without a reload. A synchronous script
  in `index.html` applies a saved Light (or auto-resolved-to-light) preference
  before first paint, so returning visitors never see a flash of dark first.
- **A real logo**, replacing the ✈ emoji: a cloud, a jet banking out of it, and
  a trend line woven through, extracted from reference artwork the user
  supplied. Alpha-matted out of two flat-color renders (not redrawn), so the
  cutout trend-line detail came through as true transparency. Two files —
  `brand-mark-for-dark.png` and `brand-mark-for-light.png` — swap in the header
  as the resolved theme changes, and the same pair drives the favicon via
  `media="(prefers-color-scheme: …)"` `<link rel="icon">` tags (a favicon
  renders outside the page DOM, so it can only follow the OS preference, not
  the in-app override). The header wordmark now sets in Space Grotesk instead
  of the system font; body text is untouched.
- **The same logo at the top of the README**, via the `<picture>` +
  `prefers-color-scheme` pattern GitHub renders natively, so it matches
  whichever theme the viewer reads GitHub in. Assets live under `docs/`.
- **A GitHub icon** on the right of the main nav, linking to the repository.
- **A Source note on the Data page**, crediting `adsblol/globe_history_2026`
  and the adsb.lol network with links, next to the existing "what has been
  downloaded" summary.

### Changed

- The Feeders page's submit form (`aircraft.json` URL, name, contact, submit
  button) is actually styled now. It had no CSS of its own, so it was falling
  back to the bare browser default input and button next to a page that styles
  everything else — the search box it sits below now sets the bar it matches.
- The Settings page's date/time preview moved inside the **Date format**
  fieldset it previews, rather than sitting after every fieldset as a separate
  box. It had drifted visibly away from Date format once **Appearance** and
  **Live gap-fill** started landing between them; nesting it means it stays
  attached to what it explains no matter what else gets added around it.

## [1.4.0] - 2026-09-20

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

### Upgrade notes

**A new `collector` service must be added to your Compose file** — see the
example in the README. Without it nothing changes; the rest of the stack works
exactly as before.

**The feature is off by default.** Set `ENABLE_LIVE_GAPFILL=true` on *both* the
`api` and `collector` services to turn it on, and set `SUBMIT_IP_SALT` to a long
random string on `api` so submission rate limits survive a restart. With the
switch off, the feeder endpoints are never registered and nothing appears in the
UI, so upgrading changes no behaviour.

**Do not set `ALLOW_PRIVATE_FEEDERS=true` on a deployment whose UI is reachable
from the internet.** It disables the check that stops a submitted URL pointing at
your internal network. It exists for stacks whose feeders sit on the same Docker
network.

No `db/maintenance/` script this time. Migration 006 applies automatically on
processor startup and includes a one-off `UPDATE` over every `aircraft` row to
set the new `archive_seen` flag. That table is far smaller than `flights` —
seconds to a minute or so, not hours — but it is a full rewrite of `aircraft`,
so expect the processor's first start to pause before it begins polling.

**Building from source now uses the repository root as the Docker context** for
`processor`, `api` and `collector`, because they share the new `shared/` module:

```bash
docker build -f api/Dockerfile .          # not: docker build ./api
```

The `frontend` still builds from `./frontend`. Anyone pulling published images
is unaffected.

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

[Unreleased]: https://github.com/ap-andersson/sky-history/compare/v1.7.0...HEAD
[1.7.0]: https://github.com/ap-andersson/sky-history/releases/tag/v1.7.0
[1.6.0]: https://github.com/ap-andersson/sky-history/releases/tag/v1.6.0
[1.5.0]: https://github.com/ap-andersson/sky-history/releases/tag/v1.5.0
[1.4.0]: https://github.com/ap-andersson/sky-history/releases/tag/v1.4.0
[1.3.0]: https://github.com/ap-andersson/sky-history/releases/tag/v1.3.0
[1.2.0]: https://github.com/ap-andersson/sky-history/releases/tag/v1.2.0
[1.1.0]: https://github.com/ap-andersson/sky-history/releases/tag/v1.1.0
