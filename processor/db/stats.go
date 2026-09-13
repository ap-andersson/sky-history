package db

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// periodTypes are the period granularities the API exposes, and therefore the
// ones period_aircraft holds precomputed counts for.
var periodTypes = []string{"day", "week", "month", "year"}

// StatsRepo maintains the pre-aggregated statistics tables.
type StatsRepo struct {
	pool *pgxpool.Pool
}

func NewStatsRepo(pool *pgxpool.Pool) *StatsRepo {
	return &StatsRepo{pool: pool}
}

// RefreshForDate recomputes every rollup affected by the given date.
//
// All figures are recomputed from the flights table rather than accumulated
// from parse counters, which makes this idempotent: reprocessing a release
// produces the same numbers rather than doubling them. Called inside the
// release transaction, so it sees that release's rows and can never leave the
// rollups disagreeing with flights.
func (r *StatsRepo) RefreshForDate(ctx context.Context, tx pgx.Tx, date time.Time) error {
	if err := r.refreshDaily(ctx, tx, date); err != nil {
		return err
	}
	if err := r.refreshDailyTypes(ctx, tx, date); err != nil {
		return err
	}
	return r.refreshPeriodAircraft(ctx, tx, date)
}

func (r *StatsRepo) refreshDaily(ctx context.Context, tx pgx.Tx, date time.Time) error {
	_, err := tx.Exec(ctx, `
        INSERT INTO daily_stats (date, flight_count, aircraft_count, updated_at)
        SELECT $1::date, COUNT(*), COUNT(DISTINCT icao), NOW()
        FROM flights WHERE date = $1::date
        ON CONFLICT (date) DO UPDATE
           SET flight_count   = EXCLUDED.flight_count,
               aircraft_count = EXCLUDED.aircraft_count,
               updated_at     = NOW()
    `, date)
	if err != nil {
		return fmt.Errorf("refresh daily_stats for %s: %w", date.Format("2006-01-02"), err)
	}
	return nil
}

func (r *StatsRepo) refreshDailyTypes(ctx context.Context, tx pgx.Tx, date time.Time) error {
	// Replace rather than upsert: a reprocess can leave a type code with no
	// remaining flights, and an upsert would strand that row at its old count.
	if _, err := tx.Exec(ctx, `DELETE FROM daily_type_stats WHERE date = $1::date`, date); err != nil {
		return fmt.Errorf("clear daily_type_stats for %s: %w", date.Format("2006-01-02"), err)
	}

	_, err := tx.Exec(ctx, `
        INSERT INTO daily_type_stats (date, type_code, flight_count, description)
        SELECT f.date, COALESCE(NULLIF(a.type_code, ''), 'Unknown'), COUNT(*),
               MAX(NULLIF(a.description, ''))
        FROM flights f
        LEFT JOIN aircraft a ON a.icao = f.icao
        WHERE f.date = $1::date
        GROUP BY 1, 2
    `, date)
	if err != nil {
		return fmt.Errorf("refresh daily_type_stats for %s: %w", date.Format("2006-01-02"), err)
	}
	return nil
}

// refreshPeriodAircraft recomputes the distinct-aircraft count for each period
// containing the date. Period boundaries are derived in SQL so that the
// processor and the backfill script share one definition; date_trunc('week')
// is Monday-based, matching the API's week handling.
func (r *StatsRepo) refreshPeriodAircraft(ctx context.Context, tx pgx.Tx, date time.Time) error {
	for _, pt := range periodTypes {
		_, err := tx.Exec(ctx, `
            WITH bounds AS (
                SELECT CASE $1::text
                         WHEN 'day'   THEN $2::date
                         WHEN 'week'  THEN date_trunc('week',  $2::date)::date
                         WHEN 'month' THEN date_trunc('month', $2::date)::date
                         WHEN 'year'  THEN date_trunc('year',  $2::date)::date
                       END AS period_start
            ),
            span AS (
                SELECT period_start,
                       CASE $1::text
                         WHEN 'day'   THEN period_start
                         WHEN 'week'  THEN period_start + 6
                         WHEN 'month' THEN (period_start + INTERVAL '1 month' - INTERVAL '1 day')::date
                         WHEN 'year'  THEN (period_start + INTERVAL '1 year'  - INTERVAL '1 day')::date
                       END AS period_end
                FROM bounds
            )
            INSERT INTO period_aircraft (period_type, period_start, aircraft_count, updated_at)
            SELECT $1::text, s.period_start,
                   (SELECT COUNT(*) FROM aircraft a WHERE EXISTS (
                        SELECT 1 FROM flights f
                        WHERE f.icao = a.icao
                          AND f.date >= s.period_start AND f.date <= s.period_end)),
                   NOW()
            FROM span s
            ON CONFLICT (period_type, period_start) DO UPDATE
               SET aircraft_count = EXCLUDED.aircraft_count,
                   updated_at     = NOW()
        `, pt, date)
		if err != nil {
			return fmt.Errorf("refresh period_aircraft (%s) for %s: %w", pt, date.Format("2006-01-02"), err)
		}
	}
	return nil
}
