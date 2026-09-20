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
			// The type is read back from the aircraft row rather than from the
			// parsed record: UpsertBatch has already run in this transaction,
			// so the row holds the type this flight should be filed under, and
			// the two can never disagree.
			_, err := tx.Exec(ctx, `
                INSERT INTO flights (icao, callsign, date, first_seen, last_seen, aircraft_type_id)
                VALUES ($1, $2, $3, $4, $5,
                        (SELECT aircraft_type_id FROM aircraft WHERE icao = $1))
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

// FillInMissingTypes assigns a type to flights recorded before their aircraft
// had one identified.
//
// Deliberately only fills in NULLs. When an aircraft's type *changes* -- a
// reclassification, or an ICAO hex reassigned to a different airframe -- the
// older flights keep the type that was believed at the time. A hex that was a
// helicopter last year and a jet today should not have last year's flights
// retroactively become jet flights. A NULL carries no such claim, so filling
// it in loses nothing.
func (r *FlightRepo) FillInMissingTypes(ctx context.Context, tx pgx.Tx) (int64, error) {
	tag, err := tx.Exec(ctx, `
        UPDATE flights f
        SET aircraft_type_id = a.aircraft_type_id
        FROM aircraft a
        WHERE a.icao = f.icao
          AND f.aircraft_type_id IS NULL
          AND a.aircraft_type_id IS NOT NULL
    `)
	if err != nil {
		return 0, fmt.Errorf("fill in missing flight types: %w", err)
	}
	return tag.RowsAffected(), nil
}

// DeleteLiveForDate removes the collector's provisional rows for a date, so
// the archive being ingested replaces them wholesale.
//
// Replacement rather than merge, because the two sources segment flights
// differently: the archive splits on the trace's new-leg flag with a whole
// day in view, the collector on a gap timeout as the day happens. The same
// flight therefore lands on different boundaries and different first_seen
// values, which the unique key cannot recognise as a duplicate. Merging would
// quietly double-count every flight in the gap window.
//
// Called inside the release transaction, before the archive rows are inserted,
// so there is never a moment where both are visible.
func (r *FlightRepo) DeleteLiveForDate(ctx context.Context, tx pgx.Tx, date time.Time) (int64, error) {
	tag, err := tx.Exec(ctx, `
        DELETE FROM flights WHERE date = $1 AND source = 'live'
    `, date)
	if err != nil {
		return 0, fmt.Errorf("delete live flights for %s: %w", date.Format("2006-01-02"), err)
	}
	return tag.RowsAffected(), nil
}

// DeleteOrphanedLive removes live rows for any date the archive already
// covers.
//
// DeleteLiveForDate handles the common case inside the release transaction,
// but the collector is a separate process writing under READ COMMITTED: a
// flush that started before the delete can land after it, leaving live rows
// for a date that has since been archived. They would then show up alongside
// the archive's own version of the same flights. Narrow window, cheap sweep,
// so it runs every poll cycle rather than being reasoned about.
func (r *FlightRepo) DeleteOrphanedLive(ctx context.Context) (int64, error) {
	tag, err := r.pool.Exec(ctx, `
        DELETE FROM flights f
        WHERE f.source = 'live'
          AND EXISTS (SELECT 1 FROM processed_releases pr WHERE pr.date = f.date)
    `)
	if err != nil {
		return 0, fmt.Errorf("delete orphaned live flights: %w", err)
	}
	return tag.RowsAffected(), nil
}
