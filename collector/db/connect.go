package db

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Connect creates a connection pool to PostgreSQL.
//
// The collector never migrates: the processor owns the schema, and two
// services racing to apply the same migrations would be a way to corrupt it.
// If the feeders table is missing, the collector waits for the processor to
// create it rather than creating it itself.
func Connect(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse database URL: %w", err)
	}

	config.MaxConns = 5
	config.MinConns = 1

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("create connection pool: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}

	return pool, nil
}

// SchemaReady reports whether the migration that introduces feeders has run.
func SchemaReady(ctx context.Context, pool *pgxpool.Pool) (bool, error) {
	var exists bool
	err := pool.QueryRow(ctx, `
        SELECT EXISTS (
            SELECT 1 FROM information_schema.columns
            WHERE table_name = 'flights' AND column_name = 'source'
        )
    `).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("check schema: %w", err)
	}
	return exists, nil
}
