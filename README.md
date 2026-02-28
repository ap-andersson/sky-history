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

### 3. Check status

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
| GET    | `/api/stats`  | Processing statistics (total aircraft, flights, releases processed) |

### Search

| Method | Path                    | Parameters                                                             | Description                          |
|--------|-------------------------|------------------------------------------------------------------------|--------------------------------------|
| GET    | `/api/search`           | `q` (required), `limit`, `offset`                                      | Quick search by ICAO, callsign, or registration (case-insensitive) |
| GET    | `/api/search/advanced`  | `icao`, `callsign`, `date`, `date_from`, `date_to`, `limit`, `offset`  | Advanced search with combinable filters |

### Aircraft & Flights

| Method | Path                             | Parameters       | Description                                      |
|--------|----------------------------------|------------------|--------------------------------------------------|
| GET    | `/api/aircraft/{icao}`           | —                | Aircraft details by ICAO hex code                 |
| GET    | `/api/aircraft/{icao}/flights`   | `date`, `limit`, `offset` | Flights for an aircraft (optionally filtered by date) |
| GET    | `/api/flights/date/{date}`       | `limit`, `offset`| All flights on a specific date (YYYY-MM-DD)       |

**Pagination:** `limit` (1–1000, default 50) and `offset` (default 0) on all list endpoints.

---

## Database Schema

Three tables are created automatically on startup:

- **`aircraft`** — ICAO (primary key), registration, type code, description
- **`flights`** — ICAO, callsign, date, first/last seen timestamps (unique on icao+callsign+date+first_seen)
- **`processed_releases`** — Tracks which GitHub release tags have been processed

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

### Running the frontend locally

```bash
cd frontend
npm install
npm run dev
```

The Vite dev server starts on `http://localhost:5173` and proxies `/api/*` to the API service.

---

## Data Source

Flight data comes from [adsblol/globe_history_2026](https://github.com/adsblol/globe_history_2026), a community project that archives global ADS-B data collected by the [adsb.lol](https://adsb.lol) network. Each daily release contains readsb trace files for every aircraft observed that day.

---

## License

This project is provided as-is for personal/educational use. The ADS-B data is sourced from community-contributed feeds via adsb.lol.
