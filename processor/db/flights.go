package db

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sky-history/processor/models"
)

// FlightRepo handles database operations for flight records.
type FlightRepo struct {
	pool *pgxpool.Pool
}

func NewFlightRepo(pool *pgxpool.Pool) *FlightRepo {
	return &FlightRepo{pool: pool}
}

// InsertBatch inserts flight records in a batch, skipping duplicates.
func (r *FlightRepo) InsertBatch(ctx context.Context, tx pgx.Tx, date time.Time, aircraft []models.ParsedAircraft) (int, error) {
	count := 0
	for _, a := range aircraft {
		if a.ICAO == "" {
			continue
		}
		for _, f := range a.Flights {
			if f.Callsign == "" {
				continue
			}
			_, err := tx.Exec(ctx, `
                INSERT INTO flights (icao, callsign, date, first_seen, last_seen)
                VALUES ($1, $2, $3, $4, $5)
                ON CONFLICT (icao, callsign, date, first_seen) DO NOTHING
            `, a.ICAO, f.Callsign, date, f.FirstSeen, f.LastSeen)
			if err != nil {
				return count, fmt.Errorf("insert flight for %s/%s: %w", a.ICAO, f.Callsign, err)
			}
			count++
		}
	}
	return count, nil
}
