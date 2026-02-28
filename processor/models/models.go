package models

import "time"

// Aircraft represents a unique aircraft identified by ICAO hex code.
type Aircraft struct {
	ICAO         string    `json:"icao"`
	Registration string    `json:"registration,omitempty"`
	TypeCode     string    `json:"type_code,omitempty"`
	Description  string    `json:"description,omitempty"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// Flight represents a single flight segment: one aircraft using one callsign
// with observed first and last seen times on a given date.
type Flight struct {
	ID        int       `json:"id"`
	ICAO      string    `json:"icao"`
	Callsign  string    `json:"callsign"`
	Date      time.Time `json:"date"`
	FirstSeen time.Time `json:"first_seen"`
	LastSeen  time.Time `json:"last_seen"`
}

// ProcessedRelease tracks which GitHub releases have been ingested.
type ProcessedRelease struct {
	Tag           string    `json:"tag"`
	Date          time.Time `json:"date"`
	AircraftCount int       `json:"aircraft_count"`
	FlightCount   int       `json:"flight_count"`
	ProcessedAt   time.Time `json:"processed_at"`
}

// ParsedAircraft holds data extracted from a single trace JSON file.
type ParsedAircraft struct {
	ICAO         string
	Registration string
	TypeCode     string
	Description  string
	Flights      []ParsedFlight
}

// ParsedFlight holds a flight segment extracted from trace data.
type ParsedFlight struct {
	Callsign  string
	FirstSeen time.Time
	LastSeen  time.Time
}
