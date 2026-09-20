// Package models holds types specific to the processor.
//
// Aircraft and Flight live in shared/models, since the API speaks them too.
// What is here is the parser's output, which nothing outside the processor
// ever sees.
package models

import "time"

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
