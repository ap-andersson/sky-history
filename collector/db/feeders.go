package db

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Feeder is an approved data source the collector polls.
type Feeder struct {
	ID   int
	Name string
	URL  string
}

// PollResult records how one poll of one feeder went.
type PollResult struct {
	FeederID int
	At       time.Time
	Err      string
}

// FeederRepo reads the feeder roster and records polling health.
type FeederRepo struct {
	pool *pgxpool.Pool
}

func NewFeederRepo(pool *pgxpool.Pool) *FeederRepo {
	return &FeederRepo{pool: pool}
}

// ListEnabled returns the feeders an administrator has approved.
//
// Re-read on an interval rather than at startup, so flipping feeders.enabled
// in psql brings a feeder online without touching the container.
func (r *FeederRepo) ListEnabled(ctx context.Context) ([]Feeder, error) {
	rows, err := r.pool.Query(ctx, `
        SELECT id, name, url FROM feeders WHERE enabled ORDER BY id
    `)
	if err != nil {
		return nil, fmt.Errorf("list enabled feeders: %w", err)
	}
	defer rows.Close()

	var feeders []Feeder
	for rows.Next() {
		var f Feeder
		if err := rows.Scan(&f.ID, &f.Name, &f.URL); err != nil {
			return nil, fmt.Errorf("scan feeder: %w", err)
		}
		feeders = append(feeders, f)
	}
	return feeders, rows.Err()
}

// RecordPolls writes the outcome of a batch of polls.
//
// Health is persisted on the flush tick rather than after every poll: at a
// five second interval across a dozen feeders that would be a steady trickle
// of writes for information nobody reads more than once a minute.
func (r *FeederRepo) RecordPolls(ctx context.Context, results []PollResult) error {
	for _, res := range results {
		var err error
		if res.Err == "" {
			_, err = r.pool.Exec(ctx, `
                UPDATE feeders
                SET last_poll_at = $2, last_ok_at = $2, last_error = NULL, fail_streak = 0
                WHERE id = $1
            `, res.FeederID, res.At)
		} else {
			_, err = r.pool.Exec(ctx, `
                UPDATE feeders
                SET last_poll_at = $2, last_error = $3, fail_streak = fail_streak + 1
                WHERE id = $1
            `, res.FeederID, res.At, truncate(res.Err, 500))
		}
		if err != nil {
			return fmt.Errorf("record poll for feeder %d: %w", res.FeederID, err)
		}
	}
	return nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
