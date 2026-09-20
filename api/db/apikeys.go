package db

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// APIKey identifies a caller of the public API. Only what request handling
// needs to know about the key -- name is carried through so log lines can
// name which integration made a request without a second lookup.
type APIKey struct {
	ID   int
	Name string
}

// ValidateAPIKey looks up a presented key by its hash and returns its
// identity if it exists and is enabled. Returns (nil, nil) -- not an error --
// for a key that is missing, wrong, or disabled: all three are the caller's
// problem, not the server's, and get the same 401 either way so a wrong key
// cannot be distinguished from a disabled one by probing.
func (q *Queries) ValidateAPIKey(ctx context.Context, rawKey string) (*APIKey, error) {
	if rawKey == "" {
		return nil, nil
	}

	var k APIKey
	err := q.pool.QueryRow(ctx, `
        SELECT id, name FROM api_keys WHERE key_hash = $1 AND enabled
    `, hashKey(rawKey)).Scan(&k.ID, &k.Name)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("validate api key: %w", err)
	}
	return &k, nil
}

// RecordAPIKeyUsage bumps the usage counters for a key after a request it
// authorized has been served.
func (q *Queries) RecordAPIKeyUsage(ctx context.Context, id int) error {
	_, err := q.pool.Exec(ctx, `
        UPDATE api_keys SET last_used_at = NOW(), request_count = request_count + 1
        WHERE id = $1
    `, id)
	if err != nil {
		return fmt.Errorf("record api key usage: %w", err)
	}
	return nil
}

// hashKey mirrors what the operator's own minting SQL computes with
// pgcrypto's digest(key, 'sha256') -- see db/migrations/007_public_api.sql --
// so a key hashed here matches the row a human inserted by hand.
func hashKey(rawKey string) string {
	sum := sha256.Sum256([]byte(rawKey))
	return hex.EncodeToString(sum[:])
}
