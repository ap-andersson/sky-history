import { useState, useEffect, useCallback, useRef } from "preact/hooks";
import {
  search,
  advancedSearch,
  getStats,
  getAircraft,
  getFailedDates,
  getAircraftTypes,
  getPeriodStats,
  type SearchResult,
  type AdvancedSearchResult,
  type Stats,
  type ExternalLink,
  type FlightWithAircraft,
  type Aircraft,
  type FailedDate,
  type AircraftType,
  type PeriodStats,
} from "./api";

type View =
  | { kind: "home" }
  | { kind: "quickResults"; result: SearchResult }
  | { kind: "advancedResults"; result: AdvancedSearchResult }
  | { kind: "detail"; icao: string; date: string; aircraft: Aircraft | null; flights: FlightWithAircraft[]; links: ExternalLink[]; total: number; offset: number }
  | { kind: "stats"; data: PeriodStats };

// Serializable route info for history state
type Route =
  | { kind: "home" }
  | { kind: "search"; q: string; offset?: number }
  | { kind: "advanced"; icao?: string; callsign?: string; type_code?: string; date?: string; date_from?: string; date_to?: string; offset?: number }
  | { kind: "detail"; icao: string; date: string }
  | { kind: "stats"; period?: string; date?: string };

function routeToPath(route: Route): string {
  switch (route.kind) {
    case "home":
      return "/";
    case "search": {
      const p = new URLSearchParams();
      p.set("q", route.q);
      if (route.offset) p.set("offset", String(route.offset));
      return `/search?${p}`;
    }
    case "advanced": {
      const p = new URLSearchParams();
      if (route.icao) p.set("icao", route.icao);
      if (route.callsign) p.set("callsign", route.callsign);
      if (route.type_code) p.set("type_code", route.type_code);
      if (route.date) p.set("date", route.date);
      if (route.date_from) p.set("date_from", route.date_from);
      if (route.date_to) p.set("date_to", route.date_to);
      if (route.offset) p.set("offset", String(route.offset));
      return `/advanced?${p}`;
    }
    case "detail":
      return `/aircraft/${route.icao}/${route.date}`;
    case "stats": {
      const p = new URLSearchParams();
      if (route.period) p.set("period", route.period);
      if (route.date) p.set("date", route.date);
      const qs = p.toString();
      return qs ? `/stats?${qs}` : "/stats";
    }
  }
}

function parseRoute(path: string, qs: URLSearchParams): Route {
  const aircraftMatch = path.match(/^\/aircraft\/([^/]+)\/(\d{4}-\d{2}-\d{2})$/);
  if (aircraftMatch) {
    return { kind: "detail", icao: aircraftMatch[1].toUpperCase(), date: aircraftMatch[2] };
  }
  if (path === "/search" && qs.has("q")) {
    return { kind: "search", q: qs.get("q")!, offset: Number(qs.get("offset")) || 0 };
  }
  if (path === "/advanced") {
    return {
      kind: "advanced",
      icao: qs.get("icao") || undefined,
      callsign: qs.get("callsign") || undefined,
      type_code: qs.get("type_code") || undefined,
      date: qs.get("date") || undefined,
      date_from: qs.get("date_from") || undefined,
      date_to: qs.get("date_to") || undefined,
      offset: Number(qs.get("offset")) || 0,
    };
  }
  if (path === "/stats") {
    return {
      kind: "stats",
      period: qs.get("period") || undefined,
      date: qs.get("date") || undefined,
    };
  }
  return { kind: "home" };
}

export function App() {
  const [query, setQuery] = useState("");
  const [stats, setStats] = useState<Stats | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  const [view, setView] = useState<View>({ kind: "home" });
  const [failedDates, setFailedDates] = useState<FailedDate[]>([]);
  const [aircraftTypes, setAircraftTypes] = useState<AircraftType[]>([]);

  // Advanced search fields
  const [showAdvanced, setShowAdvanced] = useState(false);
  const [advIcao, setAdvIcao] = useState("");
  const [advCallsign, setAdvCallsign] = useState("");
  const [advTypeCode, setAdvTypeCode] = useState("");
  const [advDate, setAdvDate] = useState("");
  const [advDateFrom, setAdvDateFrom] = useState("");
  const [advDateTo, setAdvDateTo] = useState("");

  // Prevent pushState when navigating via popstate
  const skipPush = useRef(false);
  // Track whether user has navigated within the SPA (vs direct URL load)
  const hasNavHistory = useRef(false);

  // Push browser history when view changes
  const pushRoute = useCallback((route: Route) => {
    if (skipPush.current) {
      skipPush.current = false;
      return;
    }
    const path = routeToPath(route);
    if (window.location.pathname + window.location.search !== path) {
      history.pushState(route, "", path);
    }
    hasNavHistory.current = true;
  }, []);

  // Navigate to a route (from popstate or initial load)
  const navigateToRoute = useCallback(async (route: Route) => {
    setError("");
    switch (route.kind) {
      case "home":
        setView({ kind: "home" });
        break;
      case "search":
        setQuery(route.q);
        setLoading(true);
        try {
          const res = await search(route.q, 50, route.offset || 0);
          setView({ kind: "quickResults", result: res });
        } catch (e) {
          setError(e instanceof Error ? e.message : "Search failed");
        } finally {
          setLoading(false);
        }
        break;
      case "advanced":
        setShowAdvanced(true);
        setAdvIcao(route.icao || "");
        setAdvCallsign(route.callsign || "");
        setAdvTypeCode(route.type_code || "");
        setAdvDate(route.date || "");
        setAdvDateFrom(route.date_from || "");
        setAdvDateTo(route.date_to || "");
        setLoading(true);
        try {
          const res = await advancedSearch({
            icao: route.icao, callsign: route.callsign,
            type_code: route.type_code,
            date: route.date, date_from: route.date_from, date_to: route.date_to,
            limit: 50, offset: route.offset || 0,
          });
          setView({ kind: "advancedResults", result: res });
        } catch (e) {
          setError(e instanceof Error ? e.message : "Search failed");
        } finally {
          setLoading(false);
        }
        break;
      case "detail":
        setLoading(true);
        try {
          let aircraft: Aircraft | null = null;
          try {
            const detail = await getAircraft(route.icao);
            aircraft = detail.aircraft;
          } catch { /* continue */ }
          const res = await advancedSearch({ icao: route.icao, date: route.date, limit: 200 });
          setView({
            kind: "detail", icao: route.icao.toUpperCase(), date: route.date,
            aircraft, flights: res.flights || [], links: res.links || [],
            total: res.total, offset: 0,
          });
        } catch (e) {
          setError(e instanceof Error ? e.message : "Failed to load aircraft");
        } finally {
          setLoading(false);
        }
        break;
      case "stats":
        setLoading(true);
        try {
          const data = await getPeriodStats(route.period || "week", route.date);
          setView({ kind: "stats", data });
        } catch (e) {
          setError(e instanceof Error ? e.message : "Failed to load statistics");
        } finally {
          setLoading(false);
        }
        break;
    }
  }, []);

  // Handle browser back/forward
  useEffect(() => {
    const onPopState = (e: PopStateEvent) => {
      const route: Route = e.state || parseRoute(window.location.pathname, new URLSearchParams(window.location.search));
      navigateToRoute(route);
    };
    window.addEventListener("popstate", onPopState);
    return () => window.removeEventListener("popstate", onPopState);
  }, [navigateToRoute]);

  // On initial load, parse URL and navigate if not home
  useEffect(() => {
    const route = parseRoute(window.location.pathname, new URLSearchParams(window.location.search));
    // Replace current history entry with route state
    history.replaceState(route, "", routeToPath(route));
    if (route.kind !== "home") {
      navigateToRoute(route);
    }
  }, []);

  useEffect(() => {
    getStats().then(setStats).catch(() => {});
    getFailedDates().then(r => setFailedDates(r.failed_dates || [])).catch(() => {});
    getAircraftTypes().then(r => setAircraftTypes(r.types || [])).catch(() => {});
  }, []);

  // Quick search
  const doSearch = useCallback(
    async (q: string, offset = 0) => {
      if (!q.trim()) return;
      setLoading(true);
      setError("");
      try {
        const res = await search(q.trim(), 50, offset);
        setView({ kind: "quickResults", result: res });
        pushRoute({ kind: "search", q: q.trim(), offset });
      } catch (e) {
        setError(e instanceof Error ? e.message : "Search failed");
        setView({ kind: "home" });
      } finally {
        setLoading(false);
      }
    },
    [pushRoute]
  );

  const handleSubmit = (e: Event) => {
    e.preventDefault();
    doSearch(query);
  };

  // Advanced search
  const doAdvancedSearch = useCallback(
    async (offset = 0) => {
      if (!advIcao && !advCallsign && !advTypeCode && !advDate && !advDateFrom && !advDateTo) {
        setError("Please fill in at least one filter");
        return;
      }
      setLoading(true);
      setError("");
      try {
        const res = await advancedSearch({
          icao: advIcao || undefined,
          callsign: advCallsign || undefined,
          type_code: advTypeCode || undefined,
          date: advDate || undefined,
          date_from: advDateFrom || undefined,
          date_to: advDateTo || undefined,
          limit: 50,
          offset,
        });
        setView({ kind: "advancedResults", result: res });
        pushRoute({
          kind: "advanced",
          icao: advIcao || undefined,
          callsign: advCallsign || undefined,
          type_code: advTypeCode || undefined,
          date: advDate || undefined,
          date_from: advDateFrom || undefined,
          date_to: advDateTo || undefined,
          offset,
        });
      } catch (e) {
        setError(e instanceof Error ? e.message : "Search failed");
      } finally {
        setLoading(false);
      }
    },
    [advIcao, advCallsign, advTypeCode, advDate, advDateFrom, advDateTo, pushRoute]
  );

  const handleAdvancedSubmit = (e: Event) => {
    e.preventDefault();
    doAdvancedSearch();
  };

  // View aircraft detail for a specific date
  const viewAircraftDate = useCallback(async (icao: string, date: string) => {
    setLoading(true);
    setError("");
    try {
      // Fetch aircraft info
      let aircraft: Aircraft | null = null;
      try {
        const detail = await getAircraft(icao);
        aircraft = detail.aircraft;
      } catch {
        // aircraft might not exist yet, continue
      }

      // Fetch flights for this aircraft on this date
      const res = await advancedSearch({ icao, date, limit: 200 });
      setView({
        kind: "detail",
        icao: icao.toUpperCase(),
        date,
        aircraft,
        flights: res.flights || [],
        links: res.links || [],
        total: res.total,
        offset: 0,
      });
      pushRoute({ kind: "detail", icao: icao.toUpperCase(), date });
    } catch (e) {
      setError(e instanceof Error ? e.message : "Failed to load aircraft");
    } finally {
      setLoading(false);
    }
  }, [pushRoute]);

  // Change date on detail view
  const changeDetailDate = useCallback(async (icao: string, newDate: string) => {
    setLoading(true);
    try {
      const res = await advancedSearch({ icao, date: newDate, limit: 200 });
      setView({
        kind: "detail",
        icao: icao.toUpperCase(),
        date: newDate,
        aircraft: view.kind === "detail" ? view.aircraft : null,
        flights: res.flights || [],
        links: res.links || [],
        total: res.total,
        offset: 0,
      });
      pushRoute({ kind: "detail", icao: icao.toUpperCase(), date: newDate });
    } catch (e) {
      setError(e instanceof Error ? e.message : "Failed to load flights");
    } finally {
      setLoading(false);
    }
  }, [view, pushRoute]);

  const goHome = () => {
    setView({ kind: "home" });
    setError("");
    pushRoute({ kind: "home" });
  };

  const goStats = useCallback(async (period?: string, date?: string) => {
    setLoading(true);
    setError("");
    try {
      const data = await getPeriodStats(period || "week", date);
      setView({ kind: "stats", data });
      pushRoute({ kind: "stats", period: data.period, date: data.start_date });
    } catch (e) {
      setError(e instanceof Error ? e.message : "Failed to load statistics");
    } finally {
      setLoading(false);
    }
  }, [pushRoute]);

  return (
    <div>
      <div class="header" onClick={goHome} style={{ cursor: "pointer" }}>
        <div class="header-left">
          <span class="plane-icon">✈</span>
          <h1>Sky History</h1>
        </div>
        <UtcClock />
      </div>

      {stats && <StatsBar stats={stats} failedDates={failedDates} onStatsClick={() => goStats()} />}

      {/* Quick Search */}
      <form class="search-box" onSubmit={handleSubmit}>
        <input
          type="text"
          placeholder="Search by callsign, ICAO hex, registration, or type..."
          value={query}
          onInput={(e) => setQuery((e.target as HTMLInputElement).value)}
          autofocus
        />
        <button type="submit" disabled={loading || !query.trim()}>
          {loading ? "..." : "Search"}
        </button>
        <button
          type="button"
          class={showAdvanced ? "toggle-btn active" : "toggle-btn"}
          onClick={() => setShowAdvanced(!showAdvanced)}
          title="Advanced Search"
        >
          ⚙
        </button>
      </form>

      {/* Advanced Search Panel */}
      {showAdvanced && (
        <form class="advanced-panel" onSubmit={handleAdvancedSubmit}>
          <div class="advanced-grid">
            <label>
              <span>ICAO Hex</span>
              <input type="text" placeholder="e.g. 4CAF2A" value={advIcao} onInput={(e) => setAdvIcao((e.target as HTMLInputElement).value)} />
            </label>
            <label>
              <span>Callsign</span>
              <input type="text" placeholder="e.g. RYR" value={advCallsign} onInput={(e) => setAdvCallsign((e.target as HTMLInputElement).value)} />
            </label>
            <label>
              <span>Aircraft Type</span>
              <input
                type="text"
                list="aircraft-types-list"
                placeholder="e.g. B738"
                value={advTypeCode}
                onInput={(e) => setAdvTypeCode((e.target as HTMLInputElement).value)}
              />
              <datalist id="aircraft-types-list">
                {aircraftTypes.map(t => (
                  <option key={t.id} value={t.type_code}>
                    {t.description ? `${t.type_code} \u2014 ${t.description}` : t.type_code}
                  </option>
                ))}
              </datalist>
            </label>
            <label>
              <span>Date</span>
              <input type="date" value={advDate} onInput={(e) => { setAdvDate((e.target as HTMLInputElement).value); setAdvDateFrom(""); setAdvDateTo(""); }} />
            </label>
            <label>
              <span>From</span>
              <input type="date" value={advDateFrom} onInput={(e) => { setAdvDateFrom((e.target as HTMLInputElement).value); setAdvDate(""); }} />
            </label>
            <label>
              <span>To</span>
              <input type="date" value={advDateTo} onInput={(e) => { setAdvDateTo((e.target as HTMLInputElement).value); setAdvDate(""); }} />
            </label>
          </div>
          <button type="submit" disabled={loading}>
            {loading ? "..." : "Search"}
          </button>
        </form>
      )}

      {error && <div class="error">{error}</div>}

      {/* Views */}
      {view.kind === "quickResults" && (
        <SearchResults
          result={view.result}
          onViewAircraft={viewAircraftDate}
          onPageChange={(offset) => doSearch(query, offset)}
          failedDates={failedDates}
        />
      )}

      {view.kind === "advancedResults" && (
        <AdvancedResults
          result={view.result}
          onViewAircraft={viewAircraftDate}
          onPageChange={(offset) => doAdvancedSearch(offset)}
          failedDates={failedDates}
        />
      )}

      {view.kind === "detail" && (
        <AircraftDetail
          icao={view.icao}
          date={view.date}
          aircraft={view.aircraft}
          flights={view.flights}
          links={view.links}
          total={view.total}
          loading={loading}
          onBack={() => history.back()}
          showBack={hasNavHistory.current}
          onDateChange={(d) => changeDetailDate(view.icao, d)}
          failedDates={failedDates}
        />
      )}

      {view.kind === "stats" && (
        <StatsPage
          data={view.data}
          loading={loading}
          onPeriodChange={(period, date) => goStats(period, date)}
        />
      )}
    </div>
  );
}

function UtcClock() {
  const [now, setNow] = useState(() => new Date().toISOString().slice(0, 19).replace("T", " "));

  useEffect(() => {
    const id = setInterval(() => {
      setNow(new Date().toISOString().slice(0, 19).replace("T", " "));
    }, 1000);
    return () => clearInterval(id);
  }, []);

  return <span class="utc-clock">{now} UTC</span>;
}

function StatsBar({ stats, failedDates, onStatsClick }: { stats: Stats; failedDates: FailedDate[]; onStatsClick: () => void }) {
  const [showFailed, setShowFailed] = useState(false);

  return (
    <div class="stats-bar-wrapper">
      <div class="stats-bar">
        <div class="stats-bar-left">
          <span>
            Days processed:{" "}
            <span class="stat-value">{stats.total_releases.toLocaleString()}</span>
          </span>
          <span>
            Aircraft:{" "}
            <span class="stat-value">{stats.total_aircraft.toLocaleString()}</span>
          </span>
          <span>
            Flights:{" "}
            <span class="stat-value">{stats.total_flights.toLocaleString()}</span>
          </span>
          {stats.oldest_date && stats.newest_date && (
            <span>
              Range:{" "}
              <span class="stat-value">
                {new Date(stats.oldest_date).toLocaleDateString()} — {new Date(stats.newest_date).toLocaleDateString()}
              </span>
            </span>
          )}
          <a href="#" class="stats-link" onClick={(e) => { e.preventDefault(); onStatsClick(); }}>📊 Statistics</a>
        </div>
        {failedDates.length > 0 && (
          <button
            class={"failed-dates-btn" + (showFailed ? " active" : "")}
            onClick={() => setShowFailed(!showFailed)}
            title={`${failedDates.length} date(s) failed processing`}
          >
            ⚠ {failedDates.length}
          </button>
        )}
      </div>
      {showFailed && failedDates.length > 0 && (
        <FailedDatesBanner dates={failedDates} />
      )}
    </div>
  );
}

function FailedDatesBanner({ dates, contextual }: { dates: FailedDate[]; contextual?: boolean }) {
  if (dates.length === 0) return null;

  return (
    <div class={`failed-dates-banner${contextual ? " contextual" : ""}`}>
      <div class="failed-dates-header">
        ⚠ {contextual
          ? `Data for ${dates.length === 1 ? "this date" : "some dates"} could not be processed`
          : `${dates.length} date(s) could not be processed`}
      </div>
      <div class="failed-dates-list">
        {dates.map(fd => (
          <div key={fd.tag} class="failed-date-item">
            <span class="failed-date">{formatDate(fd.date)}</span>
            <span class="failed-reason">{fd.last_error}</span>
          </div>
        ))}
      </div>
    </div>
  );
}

function SearchResults({
  result,
  onViewAircraft,
  onPageChange,
  failedDates,
}: {
  result: SearchResult;
  onViewAircraft: (icao: string, date: string) => void;
  onPageChange: (offset: number) => void;
  failedDates: FailedDate[];
}) {
  const isIcaoResult =
    result.type === "icao" &&
    result.results &&
    !Array.isArray(result.results);

  // Collect dates from results to check for overlap with failed dates
  const resultDates = new Set<string>();
  if (isIcaoResult) {
    const data = result.results as { aircraft: Aircraft | null; flights: FlightWithAircraft[] };
    data.flights?.forEach(f => { if (f.date) resultDates.add(f.date.substring(0, 10)); });
  } else {
    (result.results as FlightWithAircraft[])?.forEach(f => { if (f.date) resultDates.add(f.date.substring(0, 10)); });
  }
  const relevantFailed = failedDates.filter(fd => resultDates.has(fd.date));

  if (isIcaoResult) {
    const data = result.results as {
      aircraft: Aircraft | null;
      flights: FlightWithAircraft[];
    };

    return (
      <div>
        <span class="result-type">ICAO Lookup</span>
        {relevantFailed.length > 0 && <FailedDatesBanner dates={relevantFailed} contextual />}
        {data.aircraft && (
          <div class="card">
            <div class="aircraft-header">
              <div>
                <span class="icao">{data.aircraft.icao.toUpperCase()}</span>
                <span class="meta">
                  {[data.aircraft.registration, data.aircraft.type_code, data.aircraft.description]
                    .filter(Boolean)
                    .join(" / ")}
                </span>
              </div>
            </div>
          </div>
        )}
        {data.flights && data.flights.length > 0 ? (
          <>
            <h2>{result.total} flight(s) found</h2>
            <FlightTable
              flights={data.flights}
              showIcao={false}
              onViewAircraft={onViewAircraft}
            />
            <Pagination
              total={result.total}
              limit={result.limit}
              offset={result.offset}
              onChange={onPageChange}
            />
          </>
        ) : (
          <div class="empty">No flights found for this aircraft.</div>
        )}
      </div>
    );
  }

  // Callsign or registration results
  const flights = result.results as FlightWithAircraft[];

  return (
    <div>
      <span class="result-type">{result.type} search</span>
      {relevantFailed.length > 0 && <FailedDatesBanner dates={relevantFailed} contextual />}
      {flights && flights.length > 0 ? (
        <>
          <h2>{result.total} flight(s) found</h2>
          <FlightTable
            flights={flights}
            showIcao={true}
            onViewAircraft={onViewAircraft}
          />
          <Pagination
            total={result.total}
            limit={result.limit}
            offset={result.offset}
            onChange={onPageChange}
          />
        </>
      ) : (
        <div class="empty">No results found.</div>
      )}
    </div>
  );
}

function AdvancedResults({
  result,
  onViewAircraft,
  onPageChange,
  failedDates,
}: {
  result: AdvancedSearchResult;
  onViewAircraft: (icao: string, date: string) => void;
  onPageChange: (offset: number) => void;
  failedDates: FailedDate[];
}) {
  const resultDates = new Set<string>();
  result.flights?.forEach(f => { if (f.date) resultDates.add(f.date.substring(0, 10)); });
  // Also check the date filters themselves
  if (result.filters.date) resultDates.add(result.filters.date);
  const relevantFailed = failedDates.filter(fd => resultDates.has(fd.date));

  return (
    <div>
      <span class="result-type">Advanced Search</span>
      {relevantFailed.length > 0 && <FailedDatesBanner dates={relevantFailed} contextual />}
      <div class="filter-tags">
        {Object.entries(result.filters).map(([k, v]) => (
          <span key={k} class="filter-tag">{k}: {v}</span>
        ))}
      </div>
      {result.links && result.links.length > 0 && (
        <div style={{ marginBottom: "0.75rem" }}>
          <ExternalLinks links={result.links} />
        </div>
      )}
      {result.flights && result.flights.length > 0 ? (
        <>
          <h2>{result.total} flight(s) found</h2>
          <FlightTable
            flights={result.flights}
            showIcao={true}
            onViewAircraft={onViewAircraft}
          />
          <Pagination
            total={result.total}
            limit={result.limit}
            offset={result.offset}
            onChange={onPageChange}
          />
        </>
      ) : (
        <div class="empty">No results found.</div>
      )}
    </div>
  );
}

function AircraftDetail({
  icao,
  date,
  aircraft,
  flights,
  links,
  total,
  loading,
  onBack,
  showBack,
  onDateChange,
  failedDates,
}: {
  icao: string;
  date: string;
  aircraft: Aircraft | null;
  flights: FlightWithAircraft[];
  links: ExternalLink[];
  total: number;
  loading: boolean;
  onBack: () => void;
  showBack: boolean;
  onDateChange: (date: string) => void;
  failedDates: FailedDate[];
}) {
  const relevantFailed = failedDates.filter(fd => fd.date === date);

  return (
    <div>
      {showBack && (
        <div style={{ marginBottom: "1rem" }}>
          <a href="#" onClick={(e) => { e.preventDefault(); onBack(); }}>
            &larr; Back to results
          </a>
        </div>
      )}
      {relevantFailed.length > 0 && <FailedDatesBanner dates={relevantFailed} contextual />}
      <div class="card">
        <div class="aircraft-header">
          <div>
            <span class="icao">{icao}</span>
            {aircraft && (
              <span class="meta">
                {[aircraft.registration, aircraft.type_code, aircraft.description]
                  .filter(Boolean)
                  .join(" / ")}
              </span>
            )}
          </div>
        </div>

        <div class="date-picker-row">
          <button
            class="date-nav-btn"
            onClick={() => onDateChange(shiftDate(date, -1))}
            title="Previous day"
          >
            ◀
          </button>
          <label>
            Date:
            <input
              type="date"
              value={date}
              onChange={(e) => onDateChange((e.target as HTMLInputElement).value)}
            />
          </label>
          <button
            class="date-nav-btn"
            onClick={() => onDateChange(shiftDate(date, 1))}
            title="Next day"
          >
            ▶
          </button>
        </div>

        {links.length > 0 && <ExternalLinks links={links} />}
      </div>

      <h2>{total} flight(s) on {formatDate(date)}</h2>

      {loading ? (
        <div class="loading">Loading flights...</div>
      ) : flights.length > 0 ? (
        <FlightTable
          flights={flights}
          showIcao={false}
          onViewAircraft={() => {}}
        />
      ) : (
        <div class="empty">No flights recorded on this date.</div>
      )}
    </div>
  );
}

function FlightTable({
  flights,
  showIcao,
  onViewAircraft,
}: {
  flights: FlightWithAircraft[];
  showIcao: boolean;
  onViewAircraft: (icao: string, date: string) => void;
}) {
  return (
    <div class="card" style={{ padding: 0, overflow: "auto" }}>
      <table>
        <thead>
          <tr>
            {showIcao && <th>ICAO</th>}
            <th>Callsign</th>
            <th>Date</th>
            <th>First Seen</th>
            <th>Last Seen</th>
            <th>Duration</th>
            {showIcao && <th>Reg</th>}
            {showIcao && <th>Type</th>}
          </tr>
        </thead>
        <tbody>
          {flights.map((f, i) => {
            const flightDate = f.date ? f.date.substring(0, 10) : "";
            return (
              <tr key={f.id ?? i}>
                {showIcao && (
                  <td>
                    <a
                      href={`/aircraft/${encodeURIComponent(f.icao.toUpperCase())}/${flightDate}`}
                      class="mono"
                      onClick={(e) => {
                        if (e.ctrlKey || e.metaKey || e.shiftKey || e.button !== 0) return;
                        e.preventDefault();
                        onViewAircraft(f.icao, flightDate);
                      }}
                    >
                      {f.icao.toUpperCase()}
                    </a>
                  </td>
                )}
                <td class="mono">{f.callsign}</td>
                <td>{formatDate(flightDate)}</td>
                <td>{formatTime(f.first_seen)}</td>
                <td>{formatTime(f.last_seen)}</td>
                <td class="mono">{formatDuration(f.first_seen, f.last_seen)}</td>
                {showIcao && <td>{f.registration || "-"}</td>}
                {showIcao && <td>{f.type_code || "-"}</td>}
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}

function ExternalLinks({ links }: { links: ExternalLink[] }) {
  return (
    <div class="external-links">
      {links.map((link) => (
        <a
          key={link.url}
          href={link.url}
          target="_blank"
          rel="noopener noreferrer"
        >
          {link.name} ↗
        </a>
      ))}
    </div>
  );
}

function Pagination({
  total,
  limit,
  offset,
  onChange,
}: {
  total: number;
  limit: number;
  offset: number;
  onChange: (offset: number) => void;
}) {
  if (total <= limit) return null;

  const page = Math.floor(offset / limit) + 1;
  const totalPages = Math.ceil(total / limit);

  return (
    <div class="pagination">
      <button disabled={offset === 0} onClick={() => onChange(0)}>
        First
      </button>
      <button
        disabled={offset === 0}
        onClick={() => onChange(Math.max(0, offset - limit))}
      >
        Prev
      </button>
      <span style={{ padding: "0.4rem", color: "var(--text-dim)" }}>
        Page {page} of {totalPages}
      </span>
      <button
        disabled={offset + limit >= total}
        onClick={() => onChange(offset + limit)}
      >
        Next
      </button>
      <button
        disabled={offset + limit >= total}
        onClick={() => onChange((totalPages - 1) * limit)}
      >
        Last
      </button>
    </div>
  );
}

function formatDate(s: string): string {
  try {
    return new Date(s + "T00:00:00").toLocaleDateString();
  } catch {
    return s;
  }
}

function formatDateWithWeekday(s: string): string {
  try {
    const d = new Date(s + "T00:00:00");
    const date = d.toLocaleDateString();
    const weekday = d.toLocaleDateString(undefined, { weekday: "long" });
    return `${date}, ${weekday}`;
  } catch {
    return s;
  }
}

function shortCount(n: number): string {
  if (n >= 1000000) return (n / 1000000).toFixed(1) + "M";
  if (n >= 1000) return (n / 1000).toFixed(1) + "k";
  return String(n);
}

function formatTime(s: string): string {
  try {
    return new Date(s).toLocaleTimeString([], {
      hour: "2-digit",
      minute: "2-digit",
    });
  } catch {
    return s;
  }
}

function formatDuration(first: string, last: string): string {
  try {
    const ms = new Date(last).getTime() - new Date(first).getTime();
    if (ms < 0 || isNaN(ms)) return "-";
    const totalMin = Math.floor(ms / 60000);
    const h = Math.floor(totalMin / 60);
    const m = totalMin % 60;
    if (h > 0) return `${h}h ${m.toString().padStart(2, "0")}m`;
    return `${m}m`;
  } catch {
    return "-";
  }
}

function StatsPage({
  data,
  loading,
  onPeriodChange,
}: {
  data: PeriodStats;
  loading: boolean;
  onPeriodChange: (period: string, date?: string) => void;
}) {
  const periods = ["day", "week", "month", "year"] as const;
  const [typeFilter, setTypeFilter] = useState("");
  const [showAllTypes, setShowAllTypes] = useState(false);

  const shiftPeriod = (dir: -1 | 1) => {
    const start = new Date(data.start_date + "T00:00:00");
    let newDate: Date;
    switch (data.period) {
      case "day":
        newDate = new Date(start);
        newDate.setDate(start.getDate() + dir);
        break;
      case "week":
        newDate = new Date(start);
        newDate.setDate(start.getDate() + 7 * dir);
        break;
      case "month":
        newDate = new Date(start);
        newDate.setMonth(start.getMonth() + dir);
        break;
      case "year":
        newDate = new Date(start);
        newDate.setFullYear(start.getFullYear() + dir);
        break;
      default:
        return;
    }
    const y = newDate.getFullYear();
    const m = String(newDate.getMonth() + 1).padStart(2, "0");
    const d = String(newDate.getDate()).padStart(2, "0");
    setTypeFilter("");
    setShowAllTypes(false);
    onPeriodChange(data.period, `${y}-${m}-${d}`);
  };

  const periodLabel = () => {
    const s = formatDate(data.start_date);
    const e = formatDate(data.end_date);
    if (data.period === "day") return s;
    if (data.period === "month") {
      const d = new Date(data.start_date + "T00:00:00");
      return d.toLocaleDateString(undefined, { year: "numeric", month: "long" });
    }
    if (data.period === "year") {
      return new Date(data.start_date + "T00:00:00").getFullYear().toString();
    }
    return `${s} — ${e}`;
  };

  const maxCount = data.flight_series?.length
    ? Math.max(...data.flight_series.map(p => p.count))
    : 0;

  // Filter flights by type
  const allTypes = data.flights_by_type || [];
  const filterUpper = typeFilter.trim().toUpperCase();
  const filteredTypes = filterUpper
    ? allTypes.filter(t =>
        t.type_code.toUpperCase().includes(filterUpper) ||
        (t.description || "").toUpperCase().includes(filterUpper)
      )
    : allTypes;
  const displayTypes = (showAllTypes || filterUpper) ? filteredTypes : filteredTypes.slice(0, 50);
  const hasMore = !filterUpper && !showAllTypes && filteredTypes.length > 50;

  return (
    <div class="stats-page">
      <div class="period-selector">
        {periods.map(p => (
          <button
            key={p}
            class={data.period === p ? "period-btn active" : "period-btn"}
            onClick={() => { setTypeFilter(""); setShowAllTypes(false); onPeriodChange(p, data.start_date); }}
          >
            {p.charAt(0).toUpperCase() + p.slice(1)}
          </button>
        ))}
      </div>

      <div class="period-nav">
        <button class="date-nav-btn" onClick={() => shiftPeriod(-1)} title="Previous">◀</button>
        <span class="period-label">{periodLabel()}</span>
        <button class="date-nav-btn" onClick={() => shiftPeriod(1)} title="Next">▶</button>
      </div>

      {loading ? (
        <div class="loading">Loading statistics...</div>
      ) : (
        <>
          <div class="stats-cards">
            <div class="stat-card">
              <div class="stat-card-value">{data.total_flights.toLocaleString()}</div>
              <div class="stat-card-label">Flights</div>
            </div>
            <div class="stat-card">
              <div class="stat-card-value">{data.total_aircraft.toLocaleString()}</div>
              <div class="stat-card-label">Aircraft</div>
            </div>
            <div class="stat-card">
              <div class="stat-card-value">{data.days_processed}</div>
              <div class="stat-card-label">Days Processed</div>
            </div>
            {data.busiest_day && (
              <div class="stat-card">
                <div class="stat-card-value">{data.busiest_day_flights.toLocaleString()}</div>
                <div class="stat-card-label">Busiest Day</div>
                <div class="stat-card-sub">{formatDateWithWeekday(data.busiest_day)}</div>
              </div>
            )}
          </div>

          {data.flight_series && data.flight_series.length > 0 && (
            <div class="card">
              <h2>Flights per {data.period === "year" ? "Month" : "Day"}</h2>
              <div class="chart-container">
                {data.flight_series.map(p => {
                  const pct = maxCount > 0 ? (p.count / maxCount) * 100 : 0;
                  const shortLabel = data.period === "year"
                    ? new Date(p.label + "-01T00:00:00").toLocaleDateString(undefined, { month: "short" })
                    : new Date(p.label + "T00:00:00").toLocaleDateString(undefined, { day: "numeric", month: "short" });
                  return (
                    <div class="chart-bar-wrapper" key={p.label} title={`${p.label}: ${p.count.toLocaleString()} flights`}>
                      <div class="chart-bar" style={{ height: `${pct}%` }} />
                      <span class="chart-label">{shortLabel}</span>
                      <span class="chart-count">{shortCount(p.count)}</span>
                    </div>
                  );
                })}
              </div>
            </div>
          )}

          {allTypes.length > 0 && (
            <div class="card" style={{ padding: 0, overflow: "auto" }}>
              <div class="type-table-header">
                <h2>Flights by Aircraft Type</h2>
                <input
                  type="text"
                  class="type-filter-input"
                  placeholder="Filter types..."
                  value={typeFilter}
                  onInput={(e) => setTypeFilter((e.target as HTMLInputElement).value)}
                />
              </div>
              <table>
                <thead>
                  <tr>
                    <th>Type</th>
                    <th>Description</th>
                    <th>Flights</th>
                  </tr>
                </thead>
                <tbody>
                  {displayTypes.map((t, i) => (
                    <tr key={i}>
                      <td class="mono">{t.type_code}</td>
                      <td>{t.description || "-"}</td>
                      <td>{t.flight_count.toLocaleString()}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
              {hasMore && (
                <div class="view-all-row">
                  <button class="view-all-btn" onClick={() => setShowAllTypes(true)}>
                    View all ({filteredTypes.length})
                  </button>
                </div>
              )}
              {displayTypes.length === 0 && (
                <div class="empty" style={{ padding: "1rem" }}>No matching types</div>
              )}
            </div>
          )}
        </>
      )}
    </div>
  );
}

function shiftDate(dateStr: string, days: number): string {
  const d = new Date(dateStr + "T00:00:00");
  d.setDate(d.getDate() + days);
  const y = d.getFullYear();
  const m = String(d.getMonth() + 1).padStart(2, "0");
  const day = String(d.getDate()).padStart(2, "0");
  return `${y}-${m}-${day}`;
}
