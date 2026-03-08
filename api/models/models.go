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
	OldestDate    *time.Time `json:"oldest_date,omitempty"`
	NewestDate    *time.Time `json:"newest_date,omitempty"`
}

// ExternalLink represents a link to an external tar1090 instance.
type ExternalLink struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

// AircraftType represents a unique aircraft type with its count.
type AircraftType struct {
	ID            int    `json:"id"`
	TypeCode      string `json:"type_code"`
	Description   string `json:"description,omitempty"`
	AircraftCount int    `json:"aircraft_count"`
}

// PeriodStats holds aggregated statistics for a time period.
type PeriodStats struct {
	Period             string            `json:"period"`
	StartDate          string            `json:"start_date"`
	EndDate            string            `json:"end_date"`
	TotalFlights       int               `json:"total_flights"`
	TotalAircraft      int               `json:"total_aircraft"`
	DaysProcessed      int               `json:"days_processed"`
	BusiestDay         string            `json:"busiest_day,omitempty"`
	BusiestDayFlights  int               `json:"busiest_day_flights"`
	FlightsByType      []TypeFlightCount `json:"flights_by_type"`
	FlightSeries       []SeriesPoint     `json:"flight_series"`
}

// TypeFlightCount holds a type code and its flight count.
type TypeFlightCount struct {
	TypeCode    string `json:"type_code"`
	Description string `json:"description,omitempty"`
	FlightCount int    `json:"flight_count"`
}

// SeriesPoint represents one data point in a time series chart.
type SeriesPoint struct {
	Label string `json:"label"`
	Count int    `json:"count"`
}
