package db

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sky-history/api/models"
	"golang.org/x/sync/errgroup"
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
        FROM aircraft WHERE icao = $1
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

// SearchByCallsign finds flights matching a callsign.
// An exact match is tried first, since that is the common case and is served
// directly by idx_flights_callsign. Only when there is no exact match does it
// fall back to a prefix search, so "RYR" still matches "RYR1AB", "RYR25K".
func (q *Queries) SearchByCallsign(ctx context.Context, callsign string, limit, offset int) ([]models.FlightWithAircraft, int, error) {
	if limit <= 0 || limit > 1000 {
		limit = 50
	}

	results, total, err := q.searchCallsign(ctx, "f.callsign = $1", callsign, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	if total > 0 {
		return results, total, nil
	}

	// LIKE rather than ILIKE: callsigns are stored uppercase and the caller
	// uppercases the query, and only LIKE can use the prefix index.
	return q.searchCallsign(ctx, "f.callsign LIKE $1", callsign+"%", limit, offset)
}

// searchCallsign runs the callsign search for a single predicate on f.callsign.
func (q *Queries) searchCallsign(ctx context.Context, cond, arg string, limit, offset int) ([]models.FlightWithAircraft, int, error) {
	var total int
	err := q.pool.QueryRow(ctx,
		"SELECT COUNT(*) FROM flights f WHERE "+cond, arg).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("count callsign search: %w", err)
	}
	if total == 0 {
		return nil, 0, nil
	}

	rows, err := q.pool.Query(ctx, `
        SELECT f.id, f.icao, f.callsign, f.date, f.first_seen, f.last_seen,
               COALESCE(a.registration, ''), COALESCE(a.type_code, ''), COALESCE(a.description, '')
        FROM flights f
        LEFT JOIN aircraft a ON a.icao = f.icao
        WHERE `+cond+`
        ORDER BY f.date DESC, f.first_seen DESC
        LIMIT $2 OFFSET $3
    `, arg, limit, offset)
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
		"SELECT COUNT(*) FROM flights WHERE icao = $1", icao).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("count flights by icao: %w", err)
	}

	rows, err := q.pool.Query(ctx, `
        SELECT id, icao, callsign, date, first_seen, last_seen
        FROM flights
        WHERE icao = $1
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
		"SELECT icao FROM aircraft WHERE registration = $1", registration).Scan(&icao)
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
		"SELECT EXISTS(SELECT 1 FROM aircraft_types WHERE type_code = $1)",
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
		"SELECT COUNT(*) FROM flights f JOIN aircraft a ON a.icao = f.icao WHERE a.type_code = $1",
		typeCode).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("count flights by type: %w", err)
	}

	rows, err := q.pool.Query(ctx, `
		SELECT f.id, f.icao, f.callsign, f.date, f.first_seen, f.last_seen,
		       COALESCE(a.registration, ''), COALESCE(a.type_code, ''), COALESCE(a.description, '')
		FROM flights f
		JOIN aircraft a ON a.icao = f.icao
		WHERE a.type_code = $1
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
		conditions = append(conditions, fmt.Sprintf("f.icao = $%d", argN))
		args = append(args, f.ICAO)
		argN++
	}
	if f.Callsign != "" {
		conditions = append(conditions, fmt.Sprintf("f.callsign LIKE $%d", argN))
		args = append(args, f.Callsign+"%")
		argN++
	}
	if f.TypeCode != "" {
		conditions = append(conditions, fmt.Sprintf("a.type_code = $%d", argN))
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
//
// The four independent aggregations run concurrently, so the wall-clock cost is
// the slowest of them rather than their sum.
func (q *Queries) GetPeriodStats(ctx context.Context, startDate, endDate time.Time, seriesGroupBy string) (*models.PeriodStats, error) {
	ps := &models.PeriodStats{
		StartDate: startDate.Format("2006-01-02"),
		EndDate:   endDate.Format("2006-01-02"),
	}

	g, gctx := errgroup.WithContext(ctx)

	// Total flights, busiest day and the time series all come from a single
	// grouped scan. Grouping by date returns one row per day -- a few hundred
	// at most -- and everything else is derived from those in Go, which is far
	// cheaper than scanning the same rows once per statistic.
	g.Go(func() error {
		return q.periodDailyCounts(gctx, ps, startDate, endDate, seriesGroupBy)
	})

	// Unique aircraft in the period. Counting matching aircraft rows is much
	// faster than COUNT(DISTINCT f.icao), which forces a sort over every
	// flight row, and is exactly equivalent: flights.icao is a foreign key
	// into aircraft, so the two describe the same set.
	g.Go(func() error {
		err := q.pool.QueryRow(gctx, `
			SELECT COUNT(*)
			FROM aircraft a
			WHERE EXISTS (
				SELECT 1 FROM flights f
				WHERE f.icao = a.icao AND f.date >= $1 AND f.date <= $2
			)
		`, startDate, endDate).Scan(&ps.TotalAircraft)
		if err != nil {
			return fmt.Errorf("period stats unique aircraft: %w", err)
		}
		return nil
	})

	g.Go(func() error {
		err := q.pool.QueryRow(gctx, `
			SELECT COUNT(DISTINCT date)
			FROM processed_releases
			WHERE date >= $1 AND date <= $2
		`, startDate, endDate).Scan(&ps.DaysProcessed)
		if err != nil {
			return fmt.Errorf("period stats days processed: %w", err)
		}
		return nil
	})

	g.Go(func() error {
		return q.periodFlightsByType(gctx, ps, startDate, endDate)
	})

	if err := g.Wait(); err != nil {
		return nil, err
	}

	return ps, nil
}

// periodDailyCounts fills in total flights, the busiest day and the time series
// from one scan grouped by date.
func (q *Queries) periodDailyCounts(ctx context.Context, ps *models.PeriodStats, startDate, endDate time.Time, seriesGroupBy string) error {
	rows, err := q.pool.Query(ctx, `
		SELECT f.date, COUNT(*)
		FROM flights f
		WHERE f.date >= $1 AND f.date <= $2
		GROUP BY f.date
		ORDER BY f.date
	`, startDate, endDate)
	if err != nil {
		return fmt.Errorf("period stats daily counts: %w", err)
	}
	defer rows.Close()

	byMonth := seriesGroupBy == "month"
	for rows.Next() {
		var day time.Time
		var count int
		if err := rows.Scan(&day, &count); err != nil {
			return fmt.Errorf("scan daily count: %w", err)
		}

		ps.TotalFlights += count

		if count > ps.BusiestDayFlights {
			ps.BusiestDayFlights = count
			ps.BusiestDay = day.Format("2006-01-02")
		}

		// Rows arrive in date order, so appending keeps the series sorted and
		// a month only ever needs adding to the last point.
		if byMonth {
			label := day.Format("2006-01")
			if n := len(ps.FlightSeries); n > 0 && ps.FlightSeries[n-1].Label == label {
				ps.FlightSeries[n-1].Count += count
				continue
			}
			ps.FlightSeries = append(ps.FlightSeries, models.SeriesPoint{Label: label, Count: count})
			continue
		}
		ps.FlightSeries = append(ps.FlightSeries, models.SeriesPoint{
			Label: day.Format("2006-01-02"),
			Count: count,
		})
	}
	return rows.Err()
}

// periodFlightsByType fills in the per-type flight breakdown.
func (q *Queries) periodFlightsByType(ctx context.Context, ps *models.PeriodStats, startDate, endDate time.Time) error {
	// Collapsing flights to one row per aircraft before joining keeps the join
	// at ~300k rows instead of one row per flight.
	rows, err := q.pool.Query(ctx, `
		SELECT COALESCE(NULLIF(a.type_code, ''), 'Unknown') AS tc,
		       COALESCE(MAX(NULLIF(a.description, '')), '') AS descr,
		       SUM(f.c)::bigint AS cnt
		FROM (
			SELECT icao, COUNT(*) AS c
			FROM flights
			WHERE date >= $1 AND date <= $2
			GROUP BY icao
		) f
		LEFT JOIN aircraft a ON a.icao = f.icao
		GROUP BY tc
		ORDER BY cnt DESC
	`, startDate, endDate)
	if err != nil {
		return fmt.Errorf("period stats flights by type: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var t models.TypeFlightCount
		var cnt int64
		if err := rows.Scan(&t.TypeCode, &t.Description, &cnt); err != nil {
			return fmt.Errorf("scan type flight count: %w", err)
		}
		t.FlightCount = int(cnt)
		ps.FlightsByType = append(ps.FlightsByType, t)
	}
	return rows.Err()
}
