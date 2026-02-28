package db

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ReleaseRepo handles database operations for tracking processed releases.
type ReleaseRepo struct {
	pool *pgxpool.Pool
}

func NewReleaseRepo(pool *pgxpool.Pool) *ReleaseRepo {
	return &ReleaseRepo{pool: pool}
}

// IsProcessed checks if a release tag has already been processed.
func (r *ReleaseRepo) IsProcessed(ctx context.Context, tag string) (bool, error) {
	var exists bool
	err := r.pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM processed_releases WHERE tag = $1)", tag).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("check processed release %s: %w", tag, err)
	}
	return exists, nil
}

// MarkProcessed records that a release has been fully processed.
func (r *ReleaseRepo) MarkProcessed(ctx context.Context, tx pgx.Tx, tag string, date time.Time, aircraftCount, flightCount int) error {
	_, err := tx.Exec(ctx, `
        INSERT INTO processed_releases (tag, date, aircraft_count, flight_count, processed_at)
        VALUES ($1, $2, $3, $4, NOW())
        ON CONFLICT (tag) DO NOTHING
    `, tag, date, aircraftCount, flightCount)
	if err != nil {
		return fmt.Errorf("mark release %s processed: %w", tag, err)
	}
	return nil
}

// GetLastProcessedDate returns the most recent date that was processed.
func (r *ReleaseRepo) GetLastProcessedDate(ctx context.Context) (*time.Time, error) {
	var date *time.Time
	err := r.pool.QueryRow(ctx, "SELECT MAX(date) FROM processed_releases").Scan(&date)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get last processed date: %w", err)
	}
	return date, nil
}

// GetStats returns processing statistics.
func (r *ReleaseRepo) GetStats(ctx context.Context) (totalReleases int, totalAircraft int, totalFlights int, err error) {
	err = r.pool.QueryRow(ctx, `
        SELECT 
            COALESCE((SELECT COUNT(*) FROM processed_releases), 0),
            COALESCE((SELECT COUNT(*) FROM aircraft), 0),
            COALESCE((SELECT COUNT(*) FROM flights), 0)
    `).Scan(&totalReleases, &totalAircraft, &totalFlights)
	return
}
