# Sky-History

> **Attribution:** This project was designed and implemented with the assistance of **Claude** (Anthropic), an AI programming assistant, via GitHub Copilot in VS Code.

Sky-History is a self-hosted application that automatically downloads daily ADS-B flight data from the [adsblol/globe_history_2026](https://github.com/adsblol/globe_history_2026) GitHub releases, parses [readsb](https://github.com/wiedehopf/readsb) trace JSON files, and stores flight summaries in PostgreSQL. It provides a REST API and a browser-based search UI to explore aircraft and flight history.

The goal is **not** to store every data point — it extracts and stores only the summary of each flight segment (ICAO, callsign, date, first/last seen times).

How up to date the archive is depends entirely on what has been published on
GitHub, which leaves the most recent day or two unsearchable. The **collector**
fills that gap from live ADS-B receivers — see
[Live gap-fill](#live-gap-fill).

---

## Architecture

Sky-History consists of five Docker containers:

| Service       | Description                                                                 |
|---------------|-----------------------------------------------------------------------------|
| **db**        | PostgreSQL 16 (Alpine) — stores aircraft, flights, and processing metadata  |
| **processor** | Go service — polls GitHub releases, downloads tarballs, parses traces       |
| **api**       | Go service — REST API serving flight/aircraft data from the database        |
| **collector** | Go service — polls approved feeders for live data covering the archive gap  |
| **frontend**  | Preact + Vite SPA served by nginx, which also reverse-proxies `/api/*`      |

```
GitHub Releases ──► Processor ──┐
  (adsblol)          (Go)       │
                                ├─► PostgreSQL ◄── API ◄── nginx ◄── Browser
ADS-B feeders ────► Collector ──┘    (pgx/v5)      (Go)    (proxy)    (Preact)
 (aircraft.json)      (Go)
```

The processor supplies the searchable history; the collector covers the gap
between the last published release and now.

### Data Flow

1. **Processor** polls GitHub releases on a configurable interval.
2. New releases (daily tarballs ~2.7 GB each) are downloaded and extracted.
3. Trace JSON files (gzip-compressed, one per aircraft) are parsed concurrently.
4. Flight summaries (ICAO, callsign, date, first/last seen) are batch-inserted into PostgreSQL.
5. **API** serves the data via RESTful endpoints.
6. **Collector** polls approved feeders and writes provisional rows for the
   window the archive has not reached yet; the processor replaces them when the
   matching release lands.
7. **Frontend** provides a dark-themed UI with five sections: **Start** (quick search or multi-filter search, and per-aircraft date-scoped detail pages), **Data** (what has been processed, and any releases that failed to parse), **Statistics**, **Feeders** (the live gap-fill roster and submit form), and **Settings**.

---

### Todo

1. Handle API request from outside the stack (requires auth etc)
2. Maybe add some more flight info?
3. An approval UI, so feeders do not have to be enabled with psql


---

## Quick Start

### 1. Clone and configure

```bash
git clone https://github.com/YOUR_USER/sky-history.git
cd sky-history
cp .env.example .env
# Edit .env with your settings (see Environment Variables below)
```

### 2. Run with Docker Compose

```bash
docker compose up -d
```

The frontend will be available at `http://localhost:8080` (or whatever `FRONTEND_PORT` is set to).

### 3. Apply the search indexes

The base schema is created automatically, but the search indexes are not. Once
the processor has ingested some data, run:

```bash
docker exec -i skyhistory-db psql -U skyhistory -d skyhistory < db/maintenance/001_search_indexes.sql
```

Skipping this leaves free-text search doing a full table scan — seconds per
query once you have a few million flights. See
[Database Performance](#database-performance).

### 4. Check status

```bash
# View logs
docker compose logs -f processor

# Health check
curl http://localhost:8080/api/health

# Processing stats
curl http://localhost:8080/api/stats
```

---

## Example `docker-compose.yml`

Below is the full production docker-compose configuration with all four services. Images are published to GitHub Container Registry by the CI workflow.

```yaml
services:
  db:
    image: postgres:16-alpine
    restart: unless-stopped
    container_name: skyhistory-db
    # Tuning for a multi-GB flights table. See "Database Performance" below.
    # Without these, PostgreSQL runs with a 128MB cache and assumes spinning
    # disks, which makes it prefer sequential scans over the search indexes.
    command:
      - postgres
      - -c
      - shared_buffers=2GB
      - -c
      - effective_cache_size=6GB
      - -c
      - work_mem=16MB
      - -c
      - maintenance_work_mem=1GB
      - -c
      - random_page_cost=1.1
      - -c
      - effective_io_concurrency=200
    # Parallel query workers allocate dynamic shared memory in /dev/shm, which
    # Docker caps at 64MB by default. Too small and queries fail with
    # "could not resize shared memory segment".
    shm_size: 1gb
    environment:
      POSTGRES_DB: skyhistory
      POSTGRES_USER: skyhistory
      POSTGRES_PASSWORD: ${POSTGRES_PASSWORD:-skyhistory}
    volumes:
      - pgdata:/var/lib/postgresql/data
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U skyhistory"]
      interval: 5s
      timeout: 3s
      retries: 5
    networks:
      - skyhistory

  processor:
    image: ghcr.io/ap-andersson/sky-history-processor:latest
    # Or build locally. The context is the repository root, not ./processor:
    # the service depends on the sibling shared/ module (see shared/README.md).
    # build:
    #   context: .
    #   dockerfile: processor/Dockerfile
    restart: unless-stopped
    container_name: skyhistory-processor
    depends_on:
      db:
        condition: service_healthy
    environment:
      DATABASE_URL: postgres://skyhistory:${POSTGRES_PASSWORD:-skyhistory}@db:5432/skyhistory?sslmode=disable
      GITHUB_TOKEN: ${GITHUB_TOKEN:-}
      GITHUB_REPO: ${GITHUB_REPO:-adsblol/globe_history_2026}
      POLL_INTERVAL: ${POLL_INTERVAL:-1h}
      BACKFILL_DAYS: ${BACKFILL_DAYS:-0}
      PARSE_WORKERS: ${PARSE_WORKERS:-4}
    volumes:
      - tmpdata:/tmp/sky-history
    networks:
      - skyhistory

  api:
    image: ghcr.io/ap-andersson/sky-history-api:latest
    # Or build locally. The context is the repository root, not ./api: the
    # service depends on the sibling shared/ module (see shared/README.md).
    # build:
    #   context: .
    #   dockerfile: api/Dockerfile
    restart: unless-stopped
    container_name: skyhistory-api
    depends_on:
      db:
        condition: service_healthy
    environment:
      DATABASE_URL: postgres://skyhistory:${POSTGRES_PASSWORD:-skyhistory}@db:5432/skyhistory?sslmode=disable
      ULTRAFEEDER_URLS: ${ULTRAFEEDER_URLS:-}
      LISTEN_ADDR: ":8081"
      ENABLE_LIVE_GAPFILL: ${ENABLE_LIVE_GAPFILL:-false}
      SUBMIT_IP_SALT: ${SUBMIT_IP_SALT:-}
      ALLOW_PRIVATE_FEEDERS: ${ALLOW_PRIVATE_FEEDERS:-false}
    networks:
      - skyhistory

  collector:
    image: ghcr.io/ap-andersson/sky-history-collector:latest
    # Or build locally. The context is the repository root, not ./collector --
    # see shared/README.md.
    # build:
    #   context: .
    #   dockerfile: collector/Dockerfile
    restart: unless-stopped
    container_name: skyhistory-collector
    depends_on:
      db:
        condition: service_healthy
    environment:
      DATABASE_URL: postgres://skyhistory:${POSTGRES_PASSWORD:-skyhistory}@db:5432/skyhistory?sslmode=disable
      ENABLE_LIVE_GAPFILL: ${ENABLE_LIVE_GAPFILL:-false}
      COLLECTOR_POLL_INTERVAL: ${COLLECTOR_POLL_INTERVAL:-5s}
      COLLECTOR_SEGMENT_GAP: ${COLLECTOR_SEGMENT_GAP:-15m}
      COLLECTOR_FLUSH_INTERVAL: ${COLLECTOR_FLUSH_INTERVAL:-30s}
      ALLOW_PRIVATE_FEEDERS: ${ALLOW_PRIVATE_FEEDERS:-false}
    networks:
      - skyhistory

  frontend:
    image: ghcr.io/ap-andersson/sky-history-frontend:latest
    # Or build locally:
    # build:
    #   context: ./frontend
    #   dockerfile: Dockerfile
    restart: unless-stopped
    container_name: skyhistory-frontend
    depends_on:
      - api
    ports:
      - "${FRONTEND_PORT:-8080}:80"
    networks:
      - skyhistory

volumes:
  pgdata:
  tmpdata:

networks:
  skyhistory:
```

---

## Environment Variables

All configuration is done via environment variables. Copy `.env.example` to `.env` and adjust as needed.

| Variable           | Default                              | Description                                                                                      |
|--------------------|--------------------------------------|--------------------------------------------------------------------------------------------------|
| `DATABASE_URL`     | *(see .env.example)*                 | PostgreSQL connection string. Uses internal Docker hostname `db` when running in Compose.         |
| `POSTGRES_PASSWORD`| `skyhistory`                         | Password for the PostgreSQL container. Interpolated into `DATABASE_URL` in docker-compose.        |
| `GITHUB_TOKEN`     | *(empty)*                            | GitHub personal access token. Optional but **recommended** — unauthenticated API is rate-limited to 60 req/hr. |
| `GITHUB_REPO`      | `adsblol/globe_history_2026`         | GitHub repository to poll for releases.                                                          |
| `POLL_INTERVAL`    | `1h`                                 | How often the processor checks for new releases (e.g. `30m`, `2h`).                              |
| `BACKFILL_DAYS`    | `0`                                  | Number of past days to backfill on first run. `0` = only process the latest release.             |
| `PARSE_WORKERS`    | `4`                                  | Number of concurrent goroutines for parsing trace files.                                         |
| `TEMP_DIR`         | OS temp directory                    | Directory for downloading/extracting release tarballs. On Windows defaults to `%TEMP%`.          |
| `KEEP_DOWNLOADS`   | `false`                              | Set to `true` to cache extracted files on disk (useful during development to avoid re-downloads). |
| `ULTRAFEEDER_URLS` | *(empty)*                            | Comma-separated base URLs for tar1090/ultrafeeder instances. Adds "View Live" links in the UI.   |
| `FRONTEND_PORT`    | `8080`                               | Host port the frontend is exposed on.                                                            |
| `ENABLE_LIVE_GAPFILL` | `false`                           | **Master switch for live gap-fill.** Off by default: no polling, no feeder endpoints, no Feeders page. Set on both `api` and `collector`. |
| `COLLECTOR_POLL_INTERVAL` | `5s`                          | How often the collector polls each approved feeder.                                              |
| `COLLECTOR_SEGMENT_GAP` | `15m`                           | How long an aircraft may go unseen before its live flight segment is closed.                     |
| `COLLECTOR_FLUSH_INTERVAL` | `30s`                        | How often live segments are written to the database.                                             |
| `COLLECTOR_FEEDER_RELOAD` | `1m`                          | How often the enabled-feeder list is re-read, so approvals take effect without a restart.        |
| `COLLECTOR_FETCH_TIMEOUT` | `15s`                         | Per-request timeout when polling a feeder.                                                       |
| `FEEDER_PROBE_TIMEOUT` | `10s`                            | Timeout when checking a newly submitted feeder.                                                  |
| `SUBMIT_IP_SALT`   | *(random per restart)*               | Secret keying the hash of a submitter's IP, used only for rate limiting. See [Live gap-fill](#live-gap-fill). |
| `ALLOW_PRIVATE_FEEDERS` | `false`                         | Allow feeder URLs on private/loopback addresses. **Unsafe on a public deployment** — see [Live gap-fill](#live-gap-fill). |

---

## Display Settings

The **Settings** page stores display preferences in the browser via
`localStorage`. They are per-browser, require no server configuration, and are
not synchronised between devices.

| Setting     | Options                                                              | Default   |
|-------------|----------------------------------------------------------------------|-----------|
| Time zone   | UTC, or the browser's own zone                                        | UTC       |
| Clock       | 24-hour, 12-hour                                                      | 24-hour   |
| Date format | `2026-02-14`, `14/02/2026`, `14.02.2026`, `02/14/2026`                | ISO       |
| Live gap-fill | Include the collector's live rows in searches                       | Off       |

The Live gap-fill setting only appears when the deployment has
`ENABLE_LIVE_GAPFILL=true`.

The time zone setting shifts **instants only** — the first and last time an
aircraft was seen. A flight keeps the UTC day its release belongs to, because
that date is what search, paging and the aircraft detail URL are keyed on, and
shifting it would file a flight under a day that disagrees with the dataset it
came from. The zone in use is shown as a tooltip on times rather than printed in
tables.

All of this is presentation except **Live gap-fill**, which widens what a
search asks the API for — see [Live gap-fill](#live-gap-fill). Times are stored
and queried in UTC regardless of what is selected here.

---

## API Endpoints

All endpoints return JSON and are accessible under `/api/`.

### Health & Stats

| Method | Path          | Description                 |
|--------|---------------|-----------------------------|
| GET    | `/api/health` | Health check (`{"status":"ok"}`) |
| GET    | `/api/stats`  | Processing statistics (total aircraft, flights, releases, oldest/newest date range) |
| GET    | `/api/failed-dates` | Dates that permanently failed processing (corrupt tarballs, etc.) |
| GET    | `/api/config` | Which optional features are enabled (`{"live_gap_fill": false}`) |

### Search

| Method | Path                    | Parameters                                                             | Description                          |
|--------|-------------------------|------------------------------------------------------------------------|--------------------------------------|
| GET    | `/api/search`           | `q` (required), `limit`, `offset`, `include_live`                      | Quick search by ICAO, callsign, registration, or aircraft type (case-insensitive) |
| GET    | `/api/search/advanced`  | `icao`, `callsign`, `type_code`, `date`, `date_from`, `date_to`, `limit`, `offset`, `include_live`  | Advanced search with combinable filters |

`include_live=true` adds the collector's live rows, which are excluded by
default. Every flight carries a `source` of `archive` or `live`.

### Feeders

Registered only when `ENABLE_LIVE_GAPFILL=true`; otherwise they return 404.

| Method | Path            | Description                                                           |
|--------|-----------------|-----------------------------------------------------------------------|
| GET    | `/api/feeders`  | Public feeder roster: name, status and last successful poll. URLs are never returned. |
| POST   | `/api/feeders`  | Submit a feeder (`{url, name, contact?}`). Validated immediately, then awaits approval. |

### Aircraft & Flights

| Method | Path                             | Parameters       | Description                                      |
|--------|----------------------------------|------------------|--------------------------------------------------|
| GET    | `/api/aircraft-types`            | —                | All aircraft types with aircraft counts            |
| GET    | `/api/aircraft/{icao}`           | —                | Aircraft details by ICAO hex code                 |
| GET    | `/api/aircraft/{icao}/flights`   | `date`, `limit`, `offset` | Flights for an aircraft (optionally filtered by date) |
| GET    | `/api/flights/date/{date}`       | `limit`, `offset`| All flights on a specific date (YYYY-MM-DD)       |

**Pagination:** `limit` (1–1000, default 50) and `offset` (default 0) on all list endpoints.

---

## Database Schema

Six tables are created automatically on startup:

- **`aircraft`** — ICAO (primary key), registration, type code, description, aircraft_type_id FK, `archive_seen` (whether a release has confirmed it; statistics count only these)
- **`aircraft_types`** — Lookup table of unique aircraft types (type code + description)
- **`flights`** — ICAO, callsign, date, first/last seen timestamps, aircraft_type_id, `source` (`archive`/`live`) and `feeder_id` (unique on icao+callsign+date+first_seen)
- **`processed_releases`** — Tracks which GitHub release tags have been processed
- **`failed_releases`** — Tracks releases that failed processing (with attempt count and permanent flag)
- **`feeders`** — Submitted ADS-B receivers, their validation result and polling health. `enabled` is the manual approval switch

Three further tables hold pre-aggregated statistics, maintained by the processor
after each successful release (see [Statistics rollups](#statistics-rollups)):

- **`daily_stats`** — one row per day: flight count and distinct aircraft count
- **`daily_type_stats`** — one row per day per aircraft type: flight count and description
- **`period_aircraft`** — distinct aircraft per day/week/month/year

Migrations in `processor/db/migrations/` run automatically on processor startup
and are tracked in a `schema_migrations` table. Index changes that require
`CREATE INDEX CONCURRENTLY` live in `db/maintenance/` and are applied manually —
see [Database Performance](#database-performance).

---

## Live gap-fill

**Off by default.** Set `ENABLE_LIVE_GAPFILL=true` on both the `api` and
`collector` services to turn it on. Until then the collector polls nothing, the
feeder endpoints are not registered, `include_live` is ignored, and neither the
Feeders page nor its setting appears in the UI — so updating an existing
deployment never starts accepting submissions from the public by surprise.

The archive only becomes searchable once adsb.lol has published a day and the
processor has ingested it, which leaves the most recent day or two invisible.
The **collector** fills that window by polling ADS-B receivers for readsb's
`aircraft.json` and writing provisional flight rows.

### What it is not

Live rows are **not** comparable to archive rows, and the UI says so. The
archive aggregates thousands of receivers worldwide; a feeder sees its own
couple of hundred kilometres of sky. Most aircraft airborne right now are
invisible to every feeder on the list, so a search over the gap finding nothing
does not mean an aircraft did not fly. This is why live rows are opt-in: they
are turned on under **Settings → Live gap-fill data**, and marked in results.

### How a flight is built

Each poll yields one sighting per aircraft. Sightings from every feeder fold
into a single segment per ICAO, so two receivers watching the same aircraft
extend one row rather than creating two — that merge is the reason accepting
more feeders improves coverage. A segment ends when the callsign changes, when
the aircraft goes unseen for `COLLECTOR_SEGMENT_GAP`, or at UTC midnight
(`flights.date` demands the split). Callsign validity uses the same rules as
the trace parser, so a live day and an archive day agree about what counts as a
flight.

Timestamps come from the collector's own clock minus each entry's `seen` age,
never from the feeder's `now` field. A crowdsourced receiver with a wrong clock
would otherwise file flights under the wrong day.

Open segments are written on every flush, so an aircraft still in the air is
searchable immediately with a `last_seen` that advances.

### Reconciliation

When the release covering a date is ingested, the processor deletes that date's
live rows inside the same transaction and the archive replaces them wholesale.
They are never merged: the two sources segment flights differently — the
archive on the trace's new-leg flag, the collector on a gap timeout — so the
same flight lands on different boundaries that no unique constraint would
recognise as a duplicate.

Two further guards keep the boundary clean. The collector refuses to insert any
row for a date that already has a processed release, and the processor sweeps
stragglers on every poll cycle.

**The statistics describe the archive only.** Flight counts come from rollups
computed over archive rows; aircraft counts use `aircraft.archive_seen`, a flag
the processor sets and the collector never does. The collector still writes
`aircraft` rows, because a live flight needs a registration and a type, but an
airframe seen only in the gap window does not count towards any total until a
release confirms it. Without this the week, month and year spans — which reach
forward into the days the collector is still writing — would make the numbers
move as the gap fills and then unfills.

### Feeders are crowdsourced

Feeders live in the `feeders` table, not in configuration. Anyone can offer one
through **Feeders** in the UI. On submission the API fetches the URL and checks
that it really serves `aircraft.json`, then records it with `enabled = FALSE`.
Nothing is polled until an administrator approves it:

```sql
UPDATE feeders SET enabled = TRUE WHERE name = 'Some feeder';
```

The collector re-reads the roster every `COLLECTOR_FEEDER_RELOAD`, so that
takes effect without a restart. Submitted URLs are visible in the database
only — the public roster shows names and status, so contributing a receiver
does not publish its endpoint.

A submission stores the URL, the name, the optional contact, the submission
time, the probe result, and an HMAC of the submitter's IP address keyed by
`SUBMIT_IP_SALT`. The address itself is never stored, and the hash is used only
to rate limit submissions. The submit form lists all of this next to the
fields, and that list is meant to stay in step with what
`api/handlers/feeders.go` actually writes.

### Security

The submit endpoint makes this server fetch a URL chosen by an untrusted
party, which is a server-side request forgery primitive unless it is guarded.
`shared/feedcheck` does the guarding, for both the API's submission probe and
the collector's polling:

- Only `http` and `https`; no credentials in the URL.
- Every address is checked inside the dialer and the **vetted IP is dialled
  directly**, which closes the DNS-rebinding window between checking a name and
  connecting to it. Redirects run back through the same dialer.
- Loopback, private, link-local, CGNAT, multicast, reserved and NAT64 ranges are
  refused, IPv4-mapped IPv6 included.
- Responses are capped at 16 MB and error messages never echo the address, so a
  failed probe is not an oracle for what is listening inside the network.
- Submissions are rate limited per address: a short in-memory burst limit and a
  24-hour limit counted in the database.

`ALLOW_PRIVATE_FEEDERS=true` disables the address check, which is needed when
feeders sit on the same Docker network. **Only set it where the submit form is
not reachable by the public**, or you have handed anyone a probe of your
internal network.

Approval remains the real control: an approved feeder is a trusted data source
and can serve fabricated `aircraft.json` to inject invented flights. The blast
radius is one gap window, since the archive replaces those rows, and
`flights.feeder_id` records which feeder opened each segment so one feeder's
output can be purged:

```sql
DELETE FROM flights WHERE feeder_id = (SELECT id FROM feeders WHERE name = 'Bad feeder');
```

---

## Database Performance

The `flights` table grows by roughly 130,000 rows per day (~4M/month). At 35M
rows it is about 2.3GB of heap plus several GB of indexes, which is past the
point where default PostgreSQL settings and a missing index stop being
survivable. Free-text search went from **8.3 seconds to 13 milliseconds** once
the issues below were addressed.

### Required indexes

`processor/db/migrations/` creates the base schema, but the search indexes are
**not** applied automatically. Run them once, manually:

```bash
psql -h <db-host> -U skyhistory -d skyhistory -f db/maintenance/001_search_indexes.sql
```

Or against a Compose deployment:

```bash
docker exec -i skyhistory-db psql -U skyhistory -d skyhistory < db/maintenance/001_search_indexes.sql
```

These live in `db/maintenance/` rather than `processor/db/migrations/` on
purpose. The migration runner wraps each file in a transaction, and
`CREATE INDEX CONCURRENTLY` cannot run inside one. `CONCURRENTLY` matters here:
a plain `CREATE INDEX` on a 35M-row table blocks the processor's writes for
several minutes, while the concurrent build lets ingest continue.

The script is idempotent (`IF NOT EXISTS`) and safe to re-run.

### Why queries must avoid `UPPER()` and `ILIKE`

Neither can be served by a B-tree index, so any query using them scans the
entire table. Both are also unnecessary here: the processor stores `icao`,
`callsign` and `type_code` already uppercased and trimmed, and every API
handler uppercases its input before querying. **Keep it that way** — adding
`UPPER(column)` or `ILIKE` to a query on `flights` silently reintroduces a full
table scan.

Prefix search needs `LIKE` *and* the `text_pattern_ops` index. The database
collation is `en_US.utf8`, under which a plain B-tree index cannot serve
`LIKE 'X%'` at all — only a `text_pattern_ops` index makes the prefix range
scannable.

### Autovacuum and statistics

At PostgreSQL's default 10% scale factor, a 35M-row table is only analyzed
every ~26 days, and that interval grows with the table. Stale statistics make
the planner mis-estimate row counts and choose bad plans. The maintenance
script lowers this for `flights`:

```sql
ALTER TABLE flights SET (
    autovacuum_analyze_scale_factor = 0.01,   -- ~every 350k rows, ~2.5 days
    autovacuum_vacuum_scale_factor  = 0.02
);
```

### Memory settings

The `command:` block in the Compose example above raises `shared_buffers` from
the 128MB default to 2GB and sets `random_page_cost=1.1` for SSD storage. The
latter matters as much as the former: at the default cost of 4, the planner
assumes random I/O is expensive and may still choose a sequential scan even
when a usable index exists. Size `shared_buffers` to roughly 25% of the memory
actually available to the container, and `effective_cache_size` to 50-75%.

### Statistics rollups

The statistics page used to aggregate over the whole `flights` table on every
request, which took ~13s for a full year and grew with the data. It now reads
three rollup tables instead, and responds in well under 100ms regardless of
period length.

The processor refreshes the rollups for the affected date inside the same
transaction that inserts the flights (`processor/db/stats.go`), so they can
never disagree with the data they summarise. Everything is recomputed **from
the `flights` table** rather than accumulated from parse counters, which makes
reprocessing a release idempotent. This adds ~2.6s to a release that already
takes minutes.

Distinct aircraft is the one figure that cannot be summed from daily rows — an
aircraft flying on 100 days counts once for the year — so `period_aircraft`
holds it per period. Only the four period types the API exposes are maintained,
and a release only touches the four periods containing its date.

To populate the tables for existing data, or to rebuild them at any time:

```bash
psql -h <db-host> -U skyhistory -d skyhistory -f db/maintenance/002_backfill_stats_rollups.sql
```

**No reprocessing of releases is required** — every figure is derived from
`flights`, which is already populated. The backfill takes about a minute on 35M
rows and is safe to re-run.

Note that aircraft type codes are corrected over time by the processor, and the
incremental refresh does not retroactively reattribute historical flights. Re-run
the backfill script if you want the breakdowns rebuilt from current type data.

### Aircraft type on the flight row

`flights.aircraft_type_id` duplicates the type from the `aircraft` table. The
duplication is deliberate.

Searching "all flights of type X, most recent first" was answered by joining
`flights` to `aircraft` and ordering by date. PostgreSQL cannot estimate how
many flights a type has, because the correlation spans two tables, so it walked
the date index looking for matches and stopping at 50. That works well for a
common type and catastrophically for a rare one: searching `SB39`, with 133
flights, took **5.5 seconds**, while `A320`, with 3.1 million, took 76ms.

With the type on the row, `idx_flights_type_date (aircraft_type_id, date DESC,
first_seen DESC)` answers the query directly in sort order and `LIMIT` stops as
soon as it has enough rows. Every type now costs the same:

| Type | Flights   | Before  | After |
|------|-----------|---------|-------|
| SB39 | 133       | 5,540ms | 11ms  |
| A306 | 58,266    | 280ms   | 14ms  |
| A320 | 3,109,890 | 510ms   | 237ms |

(`A320` is now dominated by its exact `COUNT(*)`, not by fetching rows.)

The processor sets the column at insert, reading it back from the `aircraft`
row it wrote earlier in the same transaction, so the two cannot disagree.

**When an aircraft's type changes, history is left alone.** Only flights with no
type at all are filled in, by `FillInMissingTypes` on each release. An ICAO hex
that was a helicopter last year and a business jet today should not have last
year's flights retroactively become business jet flights — the same hex gets
reassigned to new airframes over time. A NULL makes no claim about the airframe,
so filling it in loses nothing; overwriting an existing type would discard what
was actually observed.

To populate the column for existing data:

```bash
psql -h <db-host> -U skyhistory -d skyhistory -f db/maintenance/003_backfill_flight_type.sql
```

Budget a couple of hours on a 35M-row table: every row is rewritten, and each
rewrite updates every index on `flights`. The script works one month at a time
with a vacuum between, so bloat stays bounded and the processor keeps running
throughout. Expect the database to grow during the process; a
`REINDEX TABLE CONCURRENTLY flights` afterwards reclaims the churn.

### Known remaining cost

Searching by aircraft type (e.g. `A320`) takes ~340ms, almost entirely the
exact `COUNT(*)` over ~3.1M matching flights. Capping the count
(`SELECT COUNT(*) FROM (SELECT 1 FROM ... LIMIT 1001) t`) reduces this to
~4ms at the cost of showing "1000+" instead of an exact total.

---

## Local Development

You can run the processor and API locally outside Docker while keeping PostgreSQL in a container (or using a remote instance).

### Prerequisites

- Go 1.22+
- Node.js 22+ (for frontend)
- PostgreSQL 16 (Docker or remote)

### Running the processor locally

```bash
cd processor

# Set up environment (or use a .env file in the project root)
export DATABASE_URL="postgres://skyhistory:skyhistory@localhost:5432/skyhistory?sslmode=disable"
export KEEP_DOWNLOADS=true
export BACKFILL_DAYS=0

go run .
```

### Running the API locally

```bash
cd api

export DATABASE_URL="postgres://skyhistory:skyhistory@localhost:5432/skyhistory?sslmode=disable"
export LISTEN_ADDR=":8081"

go run .
```

`DATABASE_URL` can point at a remote database, which is a convenient way to test
API or frontend changes against real data without rebuilding images or touching
the deployed stack. The API is read-only, so this is safe against production.
Run `npm run dev` in `frontend/` alongside it and the Vite proxy picks up the
local API automatically.

### Running the frontend locally

```bash
cd frontend
npm install
npm run dev
```

The Vite dev server starts on `http://localhost:5173` and proxies `/api/*` to the API service.

---

## Versioning and Releases

This project follows [Semantic Versioning](https://semver.org/). Changes are
recorded in [CHANGELOG.md](CHANGELOG.md).

### Image tags

Images are published to `ghcr.io/ap-andersson/sky-history-{api,processor,frontend}`:

| Tag        | Moves when                    | Use for                                       |
|------------|-------------------------------|-----------------------------------------------|
| `1.2.3`    | never (immutable)             | Pinning an exact version. Recommended for production. |
| `1.2`      | a new 1.2.x patch is released | Automatic patch updates                        |
| `1`        | a new 1.x release             | Automatic minor + patch updates                |
| `latest`   | a new release is tagged       | Tracking stable releases                       |
| `edge`     | every push to `main`          | Testing unreleased changes                     |
| `<sha>`    | never (immutable)             | Pinning an exact commit                        |

`latest` deliberately does **not** follow `main`. If you want the development
branch, use `edge`.

### Cutting a release

1. Update `CHANGELOG.md`: move entries out of `[Unreleased]` into a new
   `## [X.Y.Z] - YYYY-MM-DD` section, and add an **Upgrade notes** subsection if
   operators need to do anything (such as running a new `db/maintenance/` script).
2. Commit to `main`.
3. Tag and push:

   ```bash
   git tag -a vX.Y.Z -m "Release vX.Y.Z"
   git push origin vX.Y.Z
   ```

The tag push builds and publishes all three images with semver tags, moves
`latest`, and creates a GitHub Release using that version's CHANGELOG section as
the release notes. If no matching CHANGELOG entry exists, the workflow falls back
to auto-generated notes.

### Upgrading a deployment

```bash
docker compose pull
docker compose up -d
```

Check the release notes for a database maintenance script before upgrading —
schema migrations in `processor/db/migrations/` apply automatically on processor
startup, but anything in `db/maintenance/` must be run by hand.

---

## Data Source

Flight data comes from [adsblol/globe_history_2026](https://github.com/adsblol/globe_history_2026), a community project that archives global ADS-B data collected by the [adsb.lol](https://adsb.lol) network. Each daily release contains readsb trace files for every aircraft observed that day.

---

## License

This project is provided as-is for personal/educational use. The ADS-B data is sourced from community-contributed feeds via adsb.lol.
