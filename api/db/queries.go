package db

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sky-history/api/models"
)

// Queries provides read-only database access for the API.
type Queries struct {
	pool *pgxpool.Pool
}

func NewQueries(pool *pgxpool.Pool) *Queries {
	return &Queries{pool: pool}
}

// GetAircraft retrieves a single aircraft by ICAO hex code (case-insensitive).
func (q *Queries) GetAircraft(ctx context.Context, icao string) (*models.Aircraft, error) {
	row := q.pool.QueryRow(ctx, `
        SELECT icao, registration, type_code, description, updated_at
        FROM aircraft WHERE UPPER(icao) = UPPER($1)
    `, icao)

	var a models.Aircraft
	err := row.Scan(&a.ICAO, &a.Registration, &a.TypeCode, &a.Description, &a.UpdatedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get aircraft %s: %w", icao, err)
	}
	return &a, nil
}

// SearchByCallsign finds flights matching a callsign pattern.
// Supports exact match and prefix match (e.g. "RYR" matches "RYR1AB", "RYR25K").
func (q *Queries) SearchByCallsign(ctx context.Context, callsign string, limit, offset int) ([]models.FlightWithAircraft, int, error) {
	if limit <= 0 || limit > 1000 {
		limit = 50
	}

	pattern := callsign + "%"

	// Count total matches
	var total int
	err := q.pool.QueryRow(ctx,
		"SELECT COUNT(*) FROM flights WHERE callsign ILIKE $1", pattern).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("count callsign search: %w", err)
	}

	rows, err := q.pool.Query(ctx, `
        SELECT f.id, f.icao, f.callsign, f.date, f.first_seen, f.last_seen,
               COALESCE(a.registration, ''), COALESCE(a.type_code, ''), COALESCE(a.description, '')
        FROM flights f
        LEFT JOIN aircraft a ON a.icao = f.icao
        WHERE f.callsign ILIKE $1
        ORDER BY f.date DESC, f.first_seen DESC
        LIMIT $2 OFFSET $3
    `, pattern, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("search flights by callsign: %w", err)
	}
	defer rows.Close()

	results, err := scanFlightsWithAircraft(rows)
	if err != nil {
		return nil, 0, err
	}
	return results, total, nil
}

// GetFlightsByICAO returns all flights for a given aircraft.
func (q *Queries) GetFlightsByICAO(ctx context.Context, icao string, limit, offset int) ([]models.Flight, int, error) {
	if limit <= 0 || limit > 1000 {
		limit = 50
	}

	var total int
	err := q.pool.QueryRow(ctx,
		"SELECT COUNT(*) FROM flights WHERE UPPER(icao) = UPPER($1)", icao).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("count flights by icao: %w", err)
	}

	rows, err := q.pool.Query(ctx, `
        SELECT id, icao, callsign, date, first_seen, last_seen
        FROM flights
        WHERE UPPER(icao) = UPPER($1)
        ORDER BY date DESC, first_seen DESC
        LIMIT $2 OFFSET $3
    `, icao, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("get flights by icao: %w", err)
	}
	defer rows.Close()

	results, err := scanFlights(rows)
	if err != nil {
		return nil, 0, err
	}
	return results, total, nil
}

// GetFlightsByDate returns all flights on a given date.
func (q *Queries) GetFlightsByDate(ctx context.Context, date time.Time, limit, offset int) ([]models.FlightWithAircraft, int, error) {
	if limit <= 0 || limit > 1000 {
		limit = 50
	}

	var total int
	err := q.pool.QueryRow(ctx,
		"SELECT COUNT(*) FROM flights WHERE date = $1", date).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("count flights by date: %w", err)
	}

	rows, err := q.pool.Query(ctx, `
        SELECT f.id, f.icao, f.callsign, f.date, f.first_seen, f.last_seen,
               COALESCE(a.registration, ''), COALESCE(a.type_code, ''), COALESCE(a.description, '')
        FROM flights f
        LEFT JOIN aircraft a ON a.icao = f.icao
        WHERE f.date = $1
        ORDER BY f.callsign, f.first_seen
        LIMIT $2 OFFSET $3
    `, date, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("get flights by date: %w", err)
	}
	defer rows.Close()

	results, err := scanFlightsWithAircraft(rows)
	if err != nil {
		return nil, 0, err
	}
	return results, total, nil
}

// SearchByRegistration finds an aircraft by registration and returns its flights.
func (q *Queries) SearchByRegistration(ctx context.Context, registration string, limit, offset int) ([]models.FlightWithAircraft, int, error) {
	if limit <= 0 || limit > 1000 {
		limit = 50
	}

	// Find the aircraft ICAO by registration
	var icao string
	err := q.pool.QueryRow(ctx,
		"SELECT icao FROM aircraft WHERE UPPER(registration) = UPPER($1)", registration).Scan(&icao)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, 0, nil
		}
		return nil, 0, fmt.Errorf("find aircraft by registration: %w", err)
	}

	var total int
	err = q.pool.QueryRow(ctx,
		"SELECT COUNT(*) FROM flights WHERE icao = $1", icao).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("count flights by registration: %w", err)
	}

	rows, err := q.pool.Query(ctx, `
        SELECT f.id, f.icao, f.callsign, f.date, f.first_seen, f.last_seen,
               COALESCE(a.registration, ''), COALESCE(a.type_code, ''), COALESCE(a.description, '')
        FROM flights f
        LEFT JOIN aircraft a ON a.icao = f.icao
        WHERE f.icao = $1
        ORDER BY f.date DESC, f.first_seen DESC
        LIMIT $2 OFFSET $3
    `, icao, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("get flights by registration: %w", err)
	}
	defer rows.Close()

	results, err := scanFlightsWithAircraft(rows)
	if err != nil {
		return nil, 0, err
	}
	return results, total, nil
}

// GetStats returns processing statistics.
func (q *Queries) GetStats(ctx context.Context) (*models.Stats, error) {
	var s models.Stats
	err := q.pool.QueryRow(ctx, `
        SELECT
            COALESCE((SELECT COUNT(*) FROM processed_releases), 0),
            COALESCE((SELECT COUNT(*) FROM aircraft), 0),
            COALESCE((SELECT COUNT(*) FROM flights), 0)
    `).Scan(&s.TotalReleases, &s.TotalAircraft, &s.TotalFlights)
	if err != nil {
		return nil, fmt.Errorf("get stats: %w", err)
	}

	var minDate, maxDate *time.Time
	err = q.pool.QueryRow(ctx, "SELECT MIN(date), MAX(date) FROM processed_releases").Scan(&minDate, &maxDate)
	if err == nil {
		if minDate != nil && !minDate.IsZero() {
			s.OldestDate = minDate
		}
		if maxDate != nil && !maxDate.IsZero() {
			s.NewestDate = maxDate
		}
	}

	return &s, nil
}

func scanFlights(rows pgx.Rows) ([]models.Flight, error) {
	var results []models.Flight
	for rows.Next() {
		var f models.Flight
		if err := rows.Scan(&f.ID, &f.ICAO, &f.Callsign, &f.Date, &f.FirstSeen, &f.LastSeen); err != nil {
			return nil, fmt.Errorf("scan flight: %w", err)
		}
		results = append(results, f)
	}
	return results, rows.Err()
}

func scanFlightsWithAircraft(rows pgx.Rows) ([]models.FlightWithAircraft, error) {
	var results []models.FlightWithAircraft
	for rows.Next() {
		var f models.FlightWithAircraft
		if err := rows.Scan(
			&f.ID, &f.ICAO, &f.Callsign, &f.Date, &f.FirstSeen, &f.LastSeen,
			&f.Registration, &f.TypeCode, &f.Description,
		); err != nil {
			return nil, fmt.Errorf("scan flight with aircraft: %w", err)
		}
		results = append(results, f)
	}
	return results, rows.Err()
}

// GetAircraftTypes returns all aircraft types with aircraft counts.
func (q *Queries) GetAircraftTypes(ctx context.Context) ([]models.AircraftType, error) {
	rows, err := q.pool.Query(ctx, `
		SELECT at.id, at.type_code, at.description, COUNT(a.icao) as aircraft_count
		FROM aircraft_types at
		LEFT JOIN aircraft a ON a.aircraft_type_id = at.id
		GROUP BY at.id, at.type_code, at.description
		ORDER BY at.type_code
	`)
	if err != nil {
		return nil, fmt.Errorf("get aircraft types: %w", err)
	}
	defer rows.Close()

	var results []models.AircraftType
	for rows.Next() {
		var t models.AircraftType
		if err := rows.Scan(&t.ID, &t.TypeCode, &t.Description, &t.AircraftCount); err != nil {
			return nil, fmt.Errorf("scan aircraft type: %w", err)
		}
		results = append(results, t)
	}
	return results, rows.Err()
}

// TypeCodeExists checks if a type code exists in the aircraft_types table.
func (q *Queries) TypeCodeExists(ctx context.Context, typeCode string) (bool, error) {
	var exists bool
	err := q.pool.QueryRow(ctx,
		"SELECT EXISTS(SELECT 1 FROM aircraft_types WHERE UPPER(type_code) = UPPER($1))",
		typeCode).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("check type code exists: %w", err)
	}
	return exists, nil
}

// SearchByType finds flights for aircraft matching a type code.
func (q *Queries) SearchByType(ctx context.Context, typeCode string, limit, offset int) ([]models.FlightWithAircraft, int, error) {
	if limit <= 0 || limit > 1000 {
		limit = 50
	}

	var total int
	err := q.pool.QueryRow(ctx,
		"SELECT COUNT(*) FROM flights f JOIN aircraft a ON a.icao = f.icao WHERE UPPER(a.type_code) = UPPER($1)",
		typeCode).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("count flights by type: %w", err)
	}

	rows, err := q.pool.Query(ctx, `
		SELECT f.id, f.icao, f.callsign, f.date, f.first_seen, f.last_seen,
		       COALESCE(a.registration, ''), COALESCE(a.type_code, ''), COALESCE(a.description, '')
		FROM flights f
		JOIN aircraft a ON a.icao = f.icao
		WHERE UPPER(a.type_code) = UPPER($1)
		ORDER BY f.date DESC, f.first_seen DESC
		LIMIT $2 OFFSET $3
	`, typeCode, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("search flights by type: %w", err)
	}
	defer rows.Close()

	results, err := scanFlightsWithAircraft(rows)
	if err != nil {
		return nil, 0, err
	}
	return results, total, nil
}

// AdvancedFilter holds the filters for the advanced search endpoint.
type AdvancedFilter struct {
	ICAO     string
	Callsign string
	TypeCode string
	Date     *time.Time
	DateFrom *time.Time
	DateTo   *time.Time
}

// FailedDate represents a date that could not be processed.
type FailedDate struct {
	Date      string `json:"date"`
	Tag       string `json:"tag"`
	LastError string `json:"last_error"`
	Attempts  int    `json:"attempts"`
}

// GetFailedDates returns all permanently failed release dates.
func (q *Queries) GetFailedDates(ctx context.Context) ([]FailedDate, error) {
	rows, err := q.pool.Query(ctx, `
        SELECT date, tag, last_error, attempt_count
        FROM failed_releases
        WHERE permanent = TRUE
        ORDER BY date DESC
    `)
	if err != nil {
		return nil, fmt.Errorf("get failed dates: %w", err)
	}
	defer rows.Close()

	var results []FailedDate
	for rows.Next() {
		var f FailedDate
		var d time.Time
		if err := rows.Scan(&d, &f.Tag, &f.LastError, &f.Attempts); err != nil {
			return nil, fmt.Errorf("scan failed date: %w", err)
		}
		f.Date = d.Format("2006-01-02")
		results = append(results, f)
	}
	return results, rows.Err()
}

// AdvancedSearch queries flights with combinable filters.
func (q *Queries) AdvancedSearch(ctx context.Context, f AdvancedFilter, limit, offset int) ([]models.FlightWithAircraft, int, error) {
	if limit <= 0 || limit > 1000 {
		limit = 50
	}

	var conditions []string
	var args []interface{}
	argN := 1

	// Track whether we need the aircraft JOIN in the count query
	needsAircraftJoin := false

	if f.ICAO != "" {
		conditions = append(conditions, fmt.Sprintf("UPPER(f.icao) = UPPER($%d)", argN))
		args = append(args, f.ICAO)
		argN++
	}
	if f.Callsign != "" {
		conditions = append(conditions, fmt.Sprintf("f.callsign ILIKE $%d", argN))
		args = append(args, f.Callsign+"%")
		argN++
	}
	if f.TypeCode != "" {
		conditions = append(conditions, fmt.Sprintf("UPPER(a.type_code) = UPPER($%d)", argN))
		args = append(args, f.TypeCode)
		argN++
		needsAircraftJoin = true
	}
	if f.Date != nil {
		conditions = append(conditions, fmt.Sprintf("f.date = $%d", argN))
		args = append(args, *f.Date)
		argN++
	}
	if f.DateFrom != nil {
		conditions = append(conditions, fmt.Sprintf("f.date >= $%d", argN))
		args = append(args, *f.DateFrom)
		argN++
	}
	if f.DateTo != nil {
		conditions = append(conditions, fmt.Sprintf("f.date <= $%d", argN))
		args = append(args, *f.DateTo)
		argN++
	}

	where := strings.Join(conditions, " AND ")

	// Count — include aircraft JOIN when filtering by type
	var total int
	var countSQL string
	if needsAircraftJoin {
		countSQL = "SELECT COUNT(*) FROM flights f JOIN aircraft a ON a.icao = f.icao WHERE " + where
	} else {
		countSQL = "SELECT COUNT(*) FROM flights f WHERE " + where
	}
	err := q.pool.QueryRow(ctx, countSQL, args...).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("advanced search count: %w", err)
	}

	// Query with aircraft join
	querySQL := fmt.Sprintf(`
		SELECT f.id, f.icao, f.callsign, f.date, f.first_seen, f.last_seen,
		       COALESCE(a.registration, ''), COALESCE(a.type_code, ''), COALESCE(a.description, '')
		FROM flights f
		LEFT JOIN aircraft a ON a.icao = f.icao
		WHERE %s
		ORDER BY f.date DESC, f.first_seen DESC
		LIMIT $%d OFFSET $%d
	`, where, argN, argN+1)

	args = append(args, limit, offset)

	rows, err := q.pool.Query(ctx, querySQL, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("advanced search query: %w", err)
	}
	defer rows.Close()

	results, err := scanFlightsWithAircraft(rows)
	if err != nil {
		return nil, 0, err
	}
	return results, total, nil
}

// GetPeriodStats returns aggregated statistics for a date range.
// seriesGroupBy should be "day" or "month" to control time series granularity.
func (q *Queries) GetPeriodStats(ctx context.Context, startDate, endDate time.Time, seriesGroupBy string) (*models.PeriodStats, error) {
	ps := &models.PeriodStats{
		StartDate: startDate.Format("2006-01-02"),
		EndDate:   endDate.Format("2006-01-02"),
	}

	// Total flights and unique aircraft in period
	err := q.pool.QueryRow(ctx, `
		SELECT COUNT(*), COUNT(DISTINCT f.icao)
		FROM flights f
		WHERE f.date >= $1 AND f.date <= $2
	`, startDate, endDate).Scan(&ps.TotalFlights, &ps.TotalAircraft)
	if err != nil {
		return nil, fmt.Errorf("period stats counts: %w", err)
	}

	// Days processed in period
	err = q.pool.QueryRow(ctx, `
		SELECT COUNT(DISTINCT date)
		FROM processed_releases
		WHERE date >= $1 AND date <= $2
	`, startDate, endDate).Scan(&ps.DaysProcessed)
	if err != nil {
		return nil, fmt.Errorf("period stats days processed: %w", err)
	}

	// Flights by aircraft type
	typeRows, err := q.pool.Query(ctx, `
		SELECT COALESCE(NULLIF(a.type_code, ''), 'Unknown') as tc,
		       COALESCE(MAX(NULLIF(a.description, '')), '') as descr,
		       COUNT(*) as cnt
		FROM flights f
		LEFT JOIN aircraft a ON a.icao = f.icao
		WHERE f.date >= $1 AND f.date <= $2
		GROUP BY tc
		ORDER BY cnt DESC
	`, startDate, endDate)
	if err != nil {
		return nil, fmt.Errorf("period stats flights by type: %w", err)
	}
	defer typeRows.Close()

	for typeRows.Next() {
		var t models.TypeFlightCount
		if err := typeRows.Scan(&t.TypeCode, &t.Description, &t.FlightCount); err != nil {
			return nil, fmt.Errorf("scan type flight count: %w", err)
		}
		ps.FlightsByType = append(ps.FlightsByType, t)
	}
	if err := typeRows.Err(); err != nil {
		return nil, err
	}

	// Busiest day in period
	var busiestDay *string
	err = q.pool.QueryRow(ctx, `
		SELECT f.date::text
		FROM flights f
		WHERE f.date >= $1 AND f.date <= $2
		GROUP BY f.date
		ORDER BY COUNT(*) DESC
		LIMIT 1
	`, startDate, endDate).Scan(&busiestDay)
	if err != nil && err != pgx.ErrNoRows {
		return nil, fmt.Errorf("period stats busiest day: %w", err)
	}
	if busiestDay != nil {
		ps.BusiestDay = *busiestDay
		// Get flight count for busiest day
		_ = q.pool.QueryRow(ctx, `
			SELECT COUNT(*) FROM flights WHERE date = $1
		`, ps.BusiestDay).Scan(&ps.BusiestDayFlights)
	}

	// Flight time series
	var seriesSQL string
	if seriesGroupBy == "month" {
		seriesSQL = `
			SELECT TO_CHAR(f.date, 'YYYY-MM') as label, COUNT(*) as cnt
			FROM flights f
			WHERE f.date >= $1 AND f.date <= $2
			GROUP BY label
			ORDER BY label
		`
	} else {
		seriesSQL = `
			SELECT f.date::text as label, COUNT(*) as cnt
			FROM flights f
			WHERE f.date >= $1 AND f.date <= $2
			GROUP BY f.date
			ORDER BY f.date
		`
	}

	seriesRows, err := q.pool.Query(ctx, seriesSQL, startDate, endDate)
	if err != nil {
		return nil, fmt.Errorf("period stats flight series: %w", err)
	}
	defer seriesRows.Close()

	for seriesRows.Next() {
		var sp models.SeriesPoint
		if err := seriesRows.Scan(&sp.Label, &sp.Count); err != nil {
			return nil, fmt.Errorf("scan series point: %w", err)
		}
		ps.FlightSeries = append(ps.FlightSeries, sp)
	}
	if err := seriesRows.Err(); err != nil {
		return nil, err
	}

	return ps, nil
}
