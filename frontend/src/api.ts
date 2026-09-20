import { getSettings } from "./settings";

const API_BASE = "/api";

export interface Stats {
  total_releases: number;
  total_aircraft: number;
  total_flights: number;
  oldest_date?: string;
  newest_date?: string;
}

export interface Aircraft {
  icao: string;
  registration?: string;
  type_code?: string;
  description?: string;
  updated_at: string;
}

export interface Flight {
  id: number;
  icao: string;
  callsign: string;
  date: string;
  first_seen: string;
  last_seen: string;
  // "archive" for adsb.lol release data, "live" for a feeder filling the gap
  // since the last release. Always present; live rows only appear at all when
  // the search asked for them.
  source: "archive" | "live";
}

export interface FlightWithAircraft extends Flight {
  registration?: string;
  type_code?: string;
  description?: string;
}

export interface ExternalLink {
  name: string;
  url: string;
}

export interface SearchResult {
  type: string;
  query: string;
  total: number;
  limit: number;
  offset: number;
  results: FlightWithAircraft[] | { aircraft: Aircraft | null; flights: Flight[] };
  links?: ExternalLink[];
}

export interface AircraftDetail {
  aircraft: Aircraft;
  links: ExternalLink[];
}

export interface AircraftFlightsResult {
  icao: string;
  total: number;
  limit: number;
  offset: number;
  flights: Flight[];
  links: ExternalLink[];
}

export interface AdvancedSearchResult {
  total: number;
  limit: number;
  offset: number;
  filters: Record<string, string>;
  flights: FlightWithAircraft[];
  links?: ExternalLink[];
}

export interface FailedDate {
  date: string;
  tag: string;
  last_error: string;
  attempts: number;
}

export interface FailedDatesResult {
  failed_dates: FailedDate[];
}

export interface AircraftType {
  id: number;
  type_code: string;
  description?: string;
  aircraft_count: number;
}

// Live rows are opt-in twice over: the deployment has to have the feature on,
// and the user has to have asked for it. The server enforces both; this only
// avoids sending a parameter that would be ignored.
function liveParam(): string {
  return getSettings().includeLive ? "&include_live=true" : "";
}

async function fetchJSON<T>(url: string): Promise<T> {
  const resp = await fetch(url);
  if (!resp.ok) {
    const body = await resp.json().catch(() => ({ error: resp.statusText }));
    throw new Error(body.error || resp.statusText);
  }
  return resp.json();
}

export function getStats(): Promise<Stats> {
  return fetchJSON(`${API_BASE}/stats`);
}

export function search(
  query: string,
  limit = 50,
  offset = 0
): Promise<SearchResult> {
  return fetchJSON(
    `${API_BASE}/search?q=${encodeURIComponent(query)}&limit=${limit}&offset=${offset}${liveParam()}`
  );
}

export function getAircraft(icao: string): Promise<AircraftDetail> {
  return fetchJSON(`${API_BASE}/aircraft/${encodeURIComponent(icao)}`);
}

export function getAircraftFlights(
  icao: string,
  limit = 50,
  offset = 0
): Promise<AircraftFlightsResult> {
  return fetchJSON(
    `${API_BASE}/aircraft/${encodeURIComponent(icao)}/flights?limit=${limit}&offset=${offset}${liveParam()}`
  );
}

export function advancedSearch(params: {
  icao?: string;
  callsign?: string;
  type_code?: string;
  date?: string;
  date_from?: string;
  date_to?: string;
  limit?: number;
  offset?: number;
}): Promise<AdvancedSearchResult> {
  const qs = new URLSearchParams();
  if (params.icao) qs.set("icao", params.icao);
  if (params.callsign) qs.set("callsign", params.callsign);
  if (params.type_code) qs.set("type_code", params.type_code);
  if (params.date) qs.set("date", params.date);
  if (params.date_from) qs.set("date_from", params.date_from);
  if (params.date_to) qs.set("date_to", params.date_to);
  if (params.limit) qs.set("limit", String(params.limit));
  if (params.offset != null) qs.set("offset", String(params.offset));
  if (getSettings().includeLive) qs.set("include_live", "true");
  return fetchJSON(`${API_BASE}/search/advanced?${qs.toString()}`);
}

export function getFailedDates(): Promise<FailedDatesResult> {
  return fetchJSON(`${API_BASE}/failed-dates`);
}

export function getAircraftTypes(): Promise<{ types: AircraftType[] }> {
  return fetchJSON(`${API_BASE}/aircraft-types`);
}

export interface TypeFlightCount {
  type_code: string;
  description?: string;
  flight_count: number;
}

export interface SeriesPoint {
  label: string;
  count: number;
}

export interface PeriodStats {
  period: string;
  start_date: string;
  end_date: string;
  total_flights: number;
  total_aircraft: number;
  days_processed: number;
  busiest_day?: string;
  busiest_day_flights: number;
  flights_by_type: TypeFlightCount[];
  flight_series: SeriesPoint[];
}

export function getPeriodStats(period: string, date?: string): Promise<PeriodStats> {
  const qs = new URLSearchParams();
  qs.set("period", period);
  if (date) qs.set("date", date);
  return fetchJSON(`${API_BASE}/stats/period?${qs.toString()}`);
}

export interface Feeder {
  name: string;
  status: "live" | "stalled" | "pending";
  submitted_at: string;
  last_ok_at?: string;
}

export interface FeedersResult {
  feeders: Feeder[];
  live_flight_count: number;
  live_from?: string;
  live_to?: string;
}

export function getFeeders(): Promise<FeedersResult> {
  return fetchJSON(`${API_BASE}/feeders`);
}

export interface FeederSubmissionResult {
  status: string;
  name: string;
  probe_aircraft: number;
  message: string;
}

/**
 * Offers a feeder for approval. The server fetches the URL to check that it
 * really serves aircraft.json before recording anything, so a rejection here
 * means the URL did not answer correctly rather than that it was disallowed.
 */
export async function submitFeeder(params: {
  url: string;
  name: string;
  contact?: string;
}): Promise<FeederSubmissionResult> {
  const resp = await fetch(`${API_BASE}/feeders`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({
      url: params.url,
      name: params.name,
      contact: params.contact || "",
    }),
  });

  const body = await resp.json().catch(() => ({ error: resp.statusText }));
  if (!resp.ok) {
    throw new Error(
      [body.error || resp.statusText, body.hint].filter(Boolean).join(" ")
    );
  }
  return body as FeederSubmissionResult;
}

export interface AppConfig {
  live_gap_fill: boolean;
}

/**
 * Which optional features this deployment has switched on. Fetched once at
 * startup so the UI can leave a feature out entirely rather than offering
 * something the API would refuse.
 */
export function getConfig(): Promise<AppConfig> {
  return fetchJSON(`${API_BASE}/config`);
}
