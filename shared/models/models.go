// Package models holds the domain rows more than one service speaks.
//
// Only the types that genuinely cross a service boundary live here. The
// processor's parser output (ParsedAircraft, ParsedFlight) stays with the
// processor, and the API's response shapes (FlightWithAircraft, Stats,
// PeriodStats, Feeder and the rest) stay with the API: moving those here would
// make every service compile the other's vocabulary.
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

	// Where the row came from: "archive" for adsb.lol release data, "live" for
	// a feeder filling the gap since the last release. Always sent, so the UI
	// can mark live rows even though a search has to opt in to return them.
	Source string `json:"source"`
}
