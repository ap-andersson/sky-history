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

// Flight represents a single flight segment.
type Flight struct {
	ID        int       `json:"id"`
	ICAO      string    `json:"icao"`
	Callsign  string    `json:"callsign"`
	Date      time.Time `json:"date"`
	FirstSeen time.Time `json:"first_seen"`
	LastSeen  time.Time `json:"last_seen"`
}

// FlightWithAircraft includes aircraft metadata alongside the flight record.
type FlightWithAircraft struct {
	Flight
	Registration string `json:"registration,omitempty"`
	TypeCode     string `json:"type_code,omitempty"`
	Description  string `json:"description,omitempty"`
}

// Stats holds overall processing statistics.
type Stats struct {
	TotalReleases int        `json:"total_releases"`
	TotalAircraft int        `json:"total_aircraft"`
	TotalFlights  int        `json:"total_flights"`
	LastProcessed *time.Time `json:"last_processed,omitempty"`
}

// ExternalLink represents a link to an external tar1090 instance.
type ExternalLink struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}
