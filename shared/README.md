# shared

Code used by more than one service.

Each service is its own Go module, and requires this one through a `replace`
directive:

```
require github.com/sky-history/shared v0.0.0
replace github.com/sky-history/shared => ../shared
```

A relative `replace` only resolves if the sibling directory is present at build
time, so **the service Dockerfiles build from the repository root**, not from
the service directory:

```bash
docker build -f api/Dockerfile .
```

Each Dockerfile copies `shared/` alongside its own source and builds from
`/src/<service>`, which keeps the relative path the same as on disk.
`.dockerignore` keeps the wider context cheap.

`processor`, `api` and `collector` all build this way. `frontend` does not
depend on this module and still builds from `./frontend`.

## Packages

- **`feedcheck`** — fetches and validates a feeder's `aircraft.json`, and
  refuses to connect to addresses that are not publicly routable. Used by the
  API when a feeder is submitted, and by the collector on every poll.
- **`config`** — finding the project `.env` file, and typed environment
  readers. The `Config` structs stay with their services: they have almost
  nothing in common beyond `DATABASE_URL`, so a shared struct would be the
  union of three unrelated sets of knobs. Imported as `env` for readability.
- **`models`** — `Aircraft` and `Flight`, the domain rows the processor writes
  and the API serves. Deliberately nothing else: the parser's `ParsedAircraft`
  stays with the processor, and response shapes like `FlightWithAircraft` and
  `PeriodStats` stay with the API. The API aliases both types, so its handlers
  still say `models.Flight`.
