package db

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Segment is one aircraft observed under one callsign on one UTC date, ready
// to be written as a live flight row.
type Segment struct {
	ICAO         string
	Callsign     string
	Registration string
	TypeCode     string
	Description  string
	Date         time.Time
	FirstSeen    time.Time
	LastSeen     time.Time
	FeederID     int
}

// FlightRepo writes live flight rows.
type FlightRepo struct {
	pool *pgxpool.Pool
}

func NewFlightRepo(pool *pgxpool.Pool) *FlightRepo {
	return &FlightRepo{pool: pool}
}

// Flush writes a batch of segments in one transaction.
//
// Both open and closed segments are passed: an open one is written on every
// flush so an aircraft still in the air is searchable straight away, its
// last_seen advancing as the flush repeats against the same row. The unique
// key on (icao, callsign, date, first_seen) makes that idempotent, because a
// segment's first_seen is fixed the moment it opens.
func (r *FlightRepo) Flush(ctx context.Context, segments []Segment) (int, error) {
	if len(segments) == 0 {
		return 0, nil
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin flush transaction: %w", err)
	}
	defer tx.Rollback(ctx) // no-op after commit

	written := 0
	for _, s := range segments {
		if s.ICAO == "" || s.Callsign == "" {
			continue
		}

		// The aircraft row first, so the flight can read its type back in the
		// same transaction -- the pattern the processor already uses, which
		// keeps a flight's type and its aircraft's type from disagreeing.
		var typeID *int
		if s.TypeCode != "" {
			var id int
			err := tx.QueryRow(ctx, `
                INSERT INTO aircraft_types (type_code, description)
                VALUES ($1, $2)
                ON CONFLICT (type_code) DO UPDATE SET
                    description = CASE
                        WHEN EXCLUDED.description != '' THEN EXCLUDED.description
                        ELSE aircraft_types.description
                    END
                RETURNING id
            `, s.TypeCode, s.Description).Scan(&id)
			if err != nil {
				return written, fmt.Errorf("ensure aircraft type %s: %w", s.TypeCode, err)
			}
			typeID = &id
		}

		// archive_seen is deliberately absent from both the column list and the
		// update: it defaults to FALSE and only the processor ever sets it, so
		// an aircraft seen only in the gap window stays out of the statistics
		// until a release confirms it. Identity fields are still filled in,
		// because the live flight row needs a registration and a type.
		_, err = tx.Exec(ctx, `
            INSERT INTO aircraft (icao, registration, type_code, description, aircraft_type_id, updated_at)
            VALUES ($1, $2, $3, $4, $5, NOW())
            ON CONFLICT (icao) DO UPDATE SET
                registration = COALESCE(NULLIF(EXCLUDED.registration, ''), aircraft.registration),
                type_code = COALESCE(NULLIF(EXCLUDED.type_code, ''), aircraft.type_code),
                description = COALESCE(NULLIF(EXCLUDED.description, ''), aircraft.description),
                aircraft_type_id = COALESCE(EXCLUDED.aircraft_type_id, aircraft.aircraft_type_id),
                updated_at = NOW()
        `, s.ICAO, s.Registration, s.TypeCode, s.Description, typeID)
		if err != nil {
			return written, fmt.Errorf("upsert aircraft %s: %w", s.ICAO, err)
		}

		// Two guards, both about not touching a day the archive owns.
		//
		// The WHERE NOT EXISTS drops the insert entirely once a release has
		// been processed for this date. The processor sweeps live rows when a
		// release lands, but a segment open across that moment would simply be
		// written again on the next flush and the sweep would never win.
		//
		// The WHERE on the update covers the row that already exists: if it is
		// archive data, a live flush must not extend its last_seen.
		tag, err := tx.Exec(ctx, `
            INSERT INTO flights (icao, callsign, date, first_seen, last_seen, aircraft_type_id, source, feeder_id)
            SELECT $1, $2, $3::date, $4, $5,
                   (SELECT aircraft_type_id FROM aircraft WHERE icao = $1),
                   'live', $6
            WHERE NOT EXISTS (
                SELECT 1 FROM processed_releases WHERE date = $3::date
            )
            ON CONFLICT (icao, callsign, date, first_seen) DO UPDATE
                SET last_seen = GREATEST(flights.last_seen, EXCLUDED.last_seen)
                WHERE flights.source = 'live'
        `, s.ICAO, s.Callsign, s.Date, s.FirstSeen, s.LastSeen, s.FeederID)
		if err != nil {
			return written, fmt.Errorf("upsert live flight %s/%s: %w", s.ICAO, s.Callsign, err)
		}
		written += int(tag.RowsAffected())
	}

	if err := tx.Commit(ctx); err != nil {
		return written, fmt.Errorf("commit flush: %w", err)
	}
	return written, nil
}
