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

	var lastDate *time.Time
	err = q.pool.QueryRow(ctx, "SELECT MAX(date) FROM processed_releases").Scan(&lastDate)
	if err == nil && lastDate != nil && !lastDate.IsZero() {
		s.LastProcessed = lastDate
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

// AdvancedFilter holds the filters for the advanced search endpoint.
type AdvancedFilter struct {
	ICAO     string
	Callsign string
	Date     *time.Time
	DateFrom *time.Time
	DateTo   *time.Time
}

// AdvancedSearch queries flights with combinable filters.
func (q *Queries) AdvancedSearch(ctx context.Context, f AdvancedFilter, limit, offset int) ([]models.FlightWithAircraft, int, error) {
	if limit <= 0 || limit > 1000 {
		limit = 50
	}

	var conditions []string
	var args []interface{}
	argN := 1

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

	// Count
	var total int
	countSQL := "SELECT COUNT(*) FROM flights f WHERE " + where
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
