package handlers

import (
	"context"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/sky-history/api/db"
)

// A request with no key, or a wrong one, never reaches the per-key limiter
// below -- so on its own it would cost an attacker nothing to spam guesses,
// each one a database lookup. This guards that path specifically, not every
// request: a valid key's traffic never touches it, however much of its own
// per-key budget it uses, so it can never be the tighter of the two limits
// the way a blanket per-address cap would be.
const (
	publicAPIAnonWindow = time.Minute
	publicAPIAnonMax    = 20
)

// PublicAPIHandler authenticates and rate-limits requests to /api/public/*.
// It wraps the same handlers the frontend calls internally rather than
// duplicating their logic -- a public search or stats response is byte for
// byte what the internal one is, just gated behind a key.
type PublicAPIHandler struct {
	queries *db.Queries

	rateLimit int
	anon      *slidingWindowLimiter
	perKey    *slidingWindowLimiter
}

func NewPublicAPIHandler(queries *db.Queries, requestsPerMinute int) *PublicAPIHandler {
	return &PublicAPIHandler{
		queries:   queries,
		rateLimit: requestsPerMinute,
		anon:      newSlidingWindowLimiter(publicAPIAnonWindow, publicAPIAnonMax),
		perKey:    newSlidingWindowLimiter(time.Minute, requestsPerMinute),
	}
}

// wrap authenticates and rate-limits a request before handing it to next,
// which is one of the API's own existing handlers (Search, Stats, ...).
// Usage bookkeeping happens after next has already written its response, so a
// slow database update never adds to a caller's latency.
func (p *PublicAPIHandler) wrap(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rawKey := r.Header.Get("X-API-Key")
		if rawKey == "" {
			// No lookup needed, so no DB cost to guard -- but still worth a
			// cheap per-address throttle rather than none at all.
			if !p.anon.allow(clientAddr(r)) {
				jsonError(w, http.StatusTooManyRequests, "too many requests; slow down and try again shortly")
				return
			}
			jsonError(w, http.StatusUnauthorized,
				"missing X-API-Key header -- see https://github.com/ap-andersson/sky-history#public-api for how to request one")
			return
		}

		key, err := p.queries.ValidateAPIKey(r.Context(), rawKey)
		if err != nil {
			log.Printf("Error validating API key: %v", err)
			jsonError(w, http.StatusInternalServerError, "could not validate API key")
			return
		}
		if key == nil {
			// This is the path a guessed key costs a database lookup, so it is
			// the one that most needs throttling.
			if !p.anon.allow(clientAddr(r)) {
				jsonError(w, http.StatusTooManyRequests, "too many requests; slow down and try again shortly")
				return
			}
			jsonError(w, http.StatusUnauthorized, "invalid or disabled API key")
			return
		}

		// A valid key from here on: only the per-key limiter below applies.
		keyID := strconv.Itoa(key.ID)
		w.Header().Set("X-RateLimit-Limit", strconv.Itoa(p.rateLimit))

		if !p.perKey.allow(keyID) {
			w.Header().Set("X-RateLimit-Remaining", "0")
			jsonError(w, http.StatusTooManyRequests, "rate limit exceeded for this API key")
			return
		}
		w.Header().Set("X-RateLimit-Remaining", strconv.Itoa(p.perKey.remaining(keyID)))

		next(w, r)

		go p.recordUsage(key.ID, key.Name)
	}
}

// recordUsage runs after the response has already gone out, on its own
// context: a slow or failing counter update must never hold up, or fail, the
// request it is describing.
func (p *PublicAPIHandler) recordUsage(id int, name string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := p.queries.RecordAPIKeyUsage(ctx, id); err != nil {
		log.Printf("Error recording usage for API key %q: %v", name, err)
	}
}
