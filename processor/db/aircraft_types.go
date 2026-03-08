package db

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// AircraftTypeRepo handles database operations for aircraft type records.
type AircraftTypeRepo struct {
	pool *pgxpool.Pool
}

func NewAircraftTypeRepo(pool *pgxpool.Pool) *AircraftTypeRepo {
	return &AircraftTypeRepo{pool: pool}
}

// EnsureType upserts an aircraft type and returns its ID.
// Non-empty description wins over empty on conflict.
func (r *AircraftTypeRepo) EnsureType(ctx context.Context, tx pgx.Tx, typeCode, description string) (int, error) {
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
	`, typeCode, description).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("ensure aircraft type %s: %w", typeCode, err)
	}
	return id, nil
}
