// Package models holds the API's response shapes.
//
// Aircraft and Flight are the domain rows, defined once in shared/models and
// aliased here so handlers and queries keep referring to models.Flight. The
// rest are view types that exist only to be serialised into a response, and
// belong to this service alone.
package models

import (
	"time"

	shared "github.com/sky-history/shared/models"
)

// Aircraft represents a unique aircraft identified by ICAO hex code.
type Aircraft = shared.Aircraft

// Flight represents a single flight segment.
type Flight = shared.Flight

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
	Period            string            `json:"period"`
	StartDate         string            `json:"start_date"`
	EndDate           string            `json:"end_date"`
	TotalFlights      int               `json:"total_flights"`
	TotalAircraft     int               `json:"total_aircraft"`
	DaysProcessed     int               `json:"days_processed"`
	BusiestDay        string            `json:"busiest_day,omitempty"`
	BusiestDayFlights int               `json:"busiest_day_flights"`
	FlightsByType     []TypeFlightCount `json:"flights_by_type"`
	FlightSeries      []SeriesPoint     `json:"flight_series"`
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

// Feeder is an ADS-B receiver as the public sees it.
//
// The URL is deliberately absent: feeders are submitted by the public, and
// publishing someone's receiver endpoint would turn a contribution into a
// standing invitation to scrape it. It is visible in the database only.
type Feeder struct {
	Name string `json:"name"`

	// "live" when approved and answering, "stalled" when approved but not
	// responding, "pending" while awaiting approval.
	Status      string     `json:"status"`
	SubmittedAt time.Time  `json:"submitted_at"`
	LastOKAt    *time.Time `json:"last_ok_at,omitempty"`
}

// FeederSubmission is a new feeder offered through the submit form.
type FeederSubmission struct {
	URL     string `json:"url"`
	Name    string `json:"name"`
	Contact string `json:"contact"`
}
