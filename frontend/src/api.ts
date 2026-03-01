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
    `${API_BASE}/search?q=${encodeURIComponent(query)}&limit=${limit}&offset=${offset}`
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
    `${API_BASE}/aircraft/${encodeURIComponent(icao)}/flights?limit=${limit}&offset=${offset}`
  );
}

export function advancedSearch(params: {
  icao?: string;
  callsign?: string;
  date?: string;
  date_from?: string;
  date_to?: string;
  limit?: number;
  offset?: number;
}): Promise<AdvancedSearchResult> {
  const qs = new URLSearchParams();
  if (params.icao) qs.set("icao", params.icao);
  if (params.callsign) qs.set("callsign", params.callsign);
  if (params.date) qs.set("date", params.date);
  if (params.date_from) qs.set("date_from", params.date_from);
  if (params.date_to) qs.set("date_to", params.date_to);
  if (params.limit) qs.set("limit", String(params.limit));
  if (params.offset != null) qs.set("offset", String(params.offset));
  return fetchJSON(`${API_BASE}/search/advanced?${qs.toString()}`);
}

export function getFailedDates(): Promise<FailedDatesResult> {
  return fetchJSON(`${API_BASE}/failed-dates`);
}
