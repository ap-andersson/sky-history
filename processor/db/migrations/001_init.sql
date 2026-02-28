-- Sky-History Database Schema

CREATE TABLE IF NOT EXISTS aircraft (
    icao TEXT PRIMARY KEY,
    registration TEXT,
    type_code TEXT,
    description TEXT,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS flights (
    id SERIAL PRIMARY KEY,
    icao TEXT NOT NULL REFERENCES aircraft(icao),
    callsign TEXT NOT NULL,
    date DATE NOT NULL,
    first_seen TIMESTAMPTZ NOT NULL,
    last_seen TIMESTAMPTZ NOT NULL,
    UNIQUE(icao, callsign, date, first_seen)
);

CREATE INDEX IF NOT EXISTS idx_flights_icao_date ON flights(icao, date);
CREATE INDEX IF NOT EXISTS idx_flights_callsign ON flights(callsign);
CREATE INDEX IF NOT EXISTS idx_flights_date ON flights(date);

CREATE TABLE IF NOT EXISTS processed_releases (
    tag TEXT PRIMARY KEY,
    date DATE NOT NULL,
    aircraft_count INTEGER NOT NULL DEFAULT 0,
    flight_count INTEGER NOT NULL DEFAULT 0,
    processed_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
