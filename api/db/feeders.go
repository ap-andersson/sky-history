package db

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/sky-history/api/models"
)

// ErrDuplicateFeeder is returned when a URL has already been submitted.
var ErrDuplicateFeeder = errors.New("that feeder has already been submitted")

// NewFeeder is a validated submission on its way into the database.
type NewFeeder struct {
	URL     string
	Name    string
	Contact string
	IPHash  string

	ValidationOK    bool
	ValidationError string
	ProbeAircraft   int
}

// ListFeeders returns the public roster.
//
// A feeder is reported as stalled once it has gone a while without a
// successful poll, which is worth showing: an approved feeder that quietly
// died is the difference between a thin gap window and an empty one.
func (q *Queries) ListFeeders(ctx context.Context) ([]models.Feeder, error) {
	rows, err := q.pool.Query(ctx, `
        SELECT name, enabled, submitted_at, last_ok_at
        FROM feeders
        WHERE enabled OR validation_ok
        ORDER BY enabled DESC, submitted_at
    `)
	if err != nil {
		return nil, fmt.Errorf("list feeders: %w", err)
	}
	defer rows.Close()

	var feeders []models.Feeder
	for rows.Next() {
		var f models.Feeder
		var enabled bool
		var lastOK *time.Time
		if err := rows.Scan(&f.Name, &enabled, &f.SubmittedAt, &lastOK); err != nil {
			return nil, fmt.Errorf("scan feeder: %w", err)
		}

		switch {
		case !enabled:
			f.Status = "pending"
		case lastOK != nil && time.Since(*lastOK) < 10*time.Minute:
			f.Status = "live"
			f.LastOKAt = lastOK
		default:
			f.Status = "stalled"
			f.LastOKAt = lastOK
		}

		feeders = append(feeders, f)
	}
	return feeders, rows.Err()
}

// InsertFeeder records a submission. It never sets enabled: a feeder becomes
// active only when an administrator flips the column by hand.
func (q *Queries) InsertFeeder(ctx context.Context, f NewFeeder) error {
	var validationErr *string
	if f.ValidationError != "" {
		validationErr = &f.ValidationError
	}
	var contact *string
	if f.Contact != "" {
		contact = &f.Contact
	}

	_, err := q.pool.Exec(ctx, `
        INSERT INTO feeders (url, name, submitter_contact, submitter_ip_hash,
                             validated_at, validation_ok, validation_error, probe_aircraft)
        VALUES ($1, $2, $3, $4, NOW(), $5, $6, $7)
    `, f.URL, f.Name, contact, f.IPHash, f.ValidationOK, validationErr, f.ProbeAircraft)
	if err != nil {
		var pgErr interface{ SQLState() string }
		if errors.As(err, &pgErr) && pgErr.SQLState() == "23505" {
			return ErrDuplicateFeeder
		}
		return fmt.Errorf("insert feeder: %w", err)
	}
	return nil
}

// FeederURLExists reports whether a URL has already been submitted. Checked
// before the probe so a resubmission does not cause another fetch.
func (q *Queries) FeederURLExists(ctx context.Context, url string) (bool, error) {
	var exists bool
	err := q.pool.QueryRow(ctx,
		"SELECT EXISTS(SELECT 1 FROM feeders WHERE url = $1)", url).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("check feeder url: %w", err)
	}
	return exists, nil
}

// RecentSubmissions counts how many feeders an address has submitted within a
// window, which is the only thing the stored IP hash is used for.
func (q *Queries) RecentSubmissions(ctx context.Context, ipHash string, window time.Duration) (int, error) {
	if ipHash == "" {
		return 0, nil
	}
	var n int
	err := q.pool.QueryRow(ctx, `
        SELECT COUNT(*) FROM feeders
        WHERE submitter_ip_hash = $1 AND submitted_at > NOW() - $2::interval
    `, ipHash, fmt.Sprintf("%d seconds", int(window.Seconds()))).Scan(&n)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return 0, fmt.Errorf("count recent submissions: %w", err)
	}
	return n, nil
}

// LiveWindow reports the span the collector's rows currently cover: the day
// after the last archived date through to the newest live row. Shown in the UI
// so the opt-in toggle can say what it would add.
func (q *Queries) LiveWindow(ctx context.Context) (*time.Time, *time.Time, int, error) {
	var from, to *time.Time
	var count int
	err := q.pool.QueryRow(ctx, `
        SELECT MIN(first_seen), MAX(last_seen), COUNT(*)
        FROM flights WHERE source = 'live'
    `).Scan(&from, &to, &count)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("live window: %w", err)
	}
	return from, to, count, nil
}
