package db

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sky-history/processor/models"
)

// AircraftRepo handles database operations for aircraft records.
type AircraftRepo struct {
	pool *pgxpool.Pool
}

func NewAircraftRepo(pool *pgxpool.Pool) *AircraftRepo {
	return &AircraftRepo{pool: pool}
}

// UpsertBatch inserts or updates multiple aircraft records.
// For aircraft with a non-empty TypeCode, it ensures the type exists in aircraft_types
// and links the aircraft to it via aircraft_type_id.
func (r *AircraftRepo) UpsertBatch(ctx context.Context, tx pgx.Tx, aircraft []models.ParsedAircraft, typeRepo *AircraftTypeRepo) (int, error) {
	if len(aircraft) == 0 {
		return 0, nil
	}

	count := 0
	for _, a := range aircraft {
		if a.ICAO == "" {
			continue
		}

		var typeID *int
		if a.TypeCode != "" {
			id, err := typeRepo.EnsureType(ctx, tx, a.TypeCode, a.Description)
			if err != nil {
				return count, fmt.Errorf("ensure type for aircraft %s: %w", a.ICAO, err)
			}
			typeID = &id
		}

		_, err := tx.Exec(ctx, `
			INSERT INTO aircraft (icao, registration, type_code, description, aircraft_type_id, updated_at)
			VALUES ($1, $2, $3, $4, $5, NOW())
			ON CONFLICT (icao) DO UPDATE SET
				registration = COALESCE(NULLIF(EXCLUDED.registration, ''), aircraft.registration),
				type_code = COALESCE(NULLIF(EXCLUDED.type_code, ''), aircraft.type_code),
				description = COALESCE(NULLIF(EXCLUDED.description, ''), aircraft.description),
				aircraft_type_id = COALESCE(EXCLUDED.aircraft_type_id, aircraft.aircraft_type_id),
				updated_at = NOW()
		`, a.ICAO, a.Registration, a.TypeCode, a.Description, typeID)
		if err != nil {
			return count, fmt.Errorf("upsert aircraft %s: %w", a.ICAO, err)
		}
		count++
	}

	return count, nil
}

// GetByICAO retrieves a single aircraft by its ICAO hex code.
func (r *AircraftRepo) GetByICAO(ctx context.Context, icao string) (*models.Aircraft, error) {
	row := r.pool.QueryRow(ctx, `
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
