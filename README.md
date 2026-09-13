# Sky-History

> **Attribution:** This project was designed and implemented with the assistance of **Claude** (Anthropic), an AI programming assistant, via GitHub Copilot in VS Code.

Sky-History is a self-hosted application that automatically downloads daily ADS-B flight data from the [adsblol/globe_history_2026](https://github.com/adsblol/globe_history_2026) GitHub releases, parses [readsb](https://github.com/wiedehopf/readsb) trace JSON files, and stores flight summaries in PostgreSQL. It provides a REST API and a browser-based search UI to explore aircraft and flight history.

The goal is **not** to store every data point — it extracts and stores only the summary of each flight segment (ICAO, callsign, date, first/last seen times).

How up to date the data is depends entirely on what data has been published on github. No live colleting of data from recievers possible now.

---

## Architecture

Sky-History consists of four Docker containers:

| Service       | Description                                                                 |
|---------------|-----------------------------------------------------------------------------|
| **db**        | PostgreSQL 16 (Alpine) — stores aircraft, flights, and processing metadata  |
| **processor** | Go service — polls GitHub releases, downloads tarballs, parses traces       |
| **api**       | Go service — REST API serving flight/aircraft data from the database        |
| **frontend**  | Preact + Vite SPA served by nginx, which also reverse-proxies `/api/*`      |

```
GitHub Releases ──► Processor ──► PostgreSQL ◄── API ◄── nginx ◄── Browser
  (adsblol)          (Go)          (pgx/v5)      (Go)    (proxy)    (Preact)
```

### Data Flow

1. **Processor** polls GitHub releases on a configurable interval.
2. New releases (daily tarballs ~2.7 GB each) are downloaded and extracted.
3. Trace JSON files (gzip-compressed, one per aircraft) are parsed concurrently.
4. Flight summaries (ICAO, callsign, date, first/last seen) are batch-inserted into PostgreSQL.
5. **API** serves the data via RESTful endpoints.
6. **Frontend** provides a dark-themed search UI with quick search, advanced multi-filter search, and per-aircraft date-scoped detail pages.

---

### Todo

1. Handle API request from outside the stack (requires auth etc)
2. Maybe add some more flight info?
3. Maybe integrate into tar1090 in some way?


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
    # Or build locally:
    # build:
    #   context: ./processor
    #   dockerfile: Dockerfile
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
    # Or build locally:
    # build:
    #   context: ./api
    #   dockerfile: Dockerfile
    restart: unless-stopped
    container_name: skyhistory-api
    depends_on:
      db:
        condition: service_healthy
    environment:
      DATABASE_URL: postgres://skyhistory:${POSTGRES_PASSWORD:-skyhistory}@db:5432/skyhistory?sslmode=disable
      ULTRAFEEDER_URLS: ${ULTRAFEEDER_URLS:-}
      LISTEN_ADDR: ":8081"
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

---

## API Endpoints

All endpoints return JSON and are accessible under `/api/`.

### Health & Stats

| Method | Path          | Description                 |
|--------|---------------|-----------------------------|
| GET    | `/api/health` | Health check (`{"status":"ok"}`) |
| GET    | `/api/stats`  | Processing statistics (total aircraft, flights, releases, oldest/newest date range) |
| GET    | `/api/failed-dates` | Dates that permanently failed processing (corrupt tarballs, etc.) |

### Search

| Method | Path                    | Parameters                                                             | Description                          |
|--------|-------------------------|------------------------------------------------------------------------|--------------------------------------|
| GET    | `/api/search`           | `q` (required), `limit`, `offset`                                      | Quick search by ICAO, callsign, registration, or aircraft type (case-insensitive) |
| GET    | `/api/search/advanced`  | `icao`, `callsign`, `type_code`, `date`, `date_from`, `date_to`, `limit`, `offset`  | Advanced search with combinable filters |

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

Five tables are created automatically on startup:

- **`aircraft`** — ICAO (primary key), registration, type code, description, aircraft_type_id FK
- **`aircraft_types`** — Lookup table of unique aircraft types (type code + description)
- **`flights`** — ICAO, callsign, date, first/last seen timestamps (unique on icao+callsign+date+first_seen)
- **`processed_releases`** — Tracks which GitHub release tags have been processed
- **`failed_releases`** — Tracks releases that failed processing (with attempt count and permanent flag)

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
