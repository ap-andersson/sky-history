package handlers

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// slidingWindowLimiter caps how many events one key may record within a
// trailing time window, held in memory. Shared by the feeder submission
// burst guard and the public API's per-address and per-key limits -- the
// same shape of problem in three places, so one implementation.
type slidingWindowLimiter struct {
	window time.Duration
	max    int

	mu     sync.Mutex
	events map[string][]time.Time
}

func newSlidingWindowLimiter(window time.Duration, max int) *slidingWindowLimiter {
	return &slidingWindowLimiter{
		window: window,
		max:    max,
		events: make(map[string][]time.Time),
	}
}

// allow reports whether key may record one more event right now, and records
// it if so. Sweeps every key's stale entries on each call rather than running
// a separate cleanup goroutine; cheap at the request volumes this runs at,
// and it keeps the map from growing without bound.
func (l *slidingWindowLimiter) allow(key string) bool {
	now := time.Now()

	l.mu.Lock()
	defer l.mu.Unlock()

	for k, times := range l.events {
		kept := times[:0]
		for _, t := range times {
			if now.Sub(t) < l.window {
				kept = append(kept, t)
			}
		}
		if len(kept) == 0 {
			delete(l.events, k)
		} else {
			l.events[k] = kept
		}
	}

	if len(l.events[key]) >= l.max {
		return false
	}
	l.events[key] = append(l.events[key], now)
	return true
}

// remaining reports how many more events key may record in the current
// window, for reporting back to a caller (e.g. an X-RateLimit-Remaining
// header). Never negative, even momentarily over the limit.
func (l *slidingWindowLimiter) remaining(key string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	n := l.max - len(l.events[key])
	if n < 0 {
		n = 0
	}
	return n
}

// clientAddr extracts the caller's address from a request reaching this
// service. X-Forwarded-For is trusted here because the frontend's nginx sets
// it and is the only thing that reaches this service directly -- exposing the
// API to the internet without that proxy in front would make the header
// forgeable, which would only defeat rate limiting, not leak anything.
func clientAddr(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		if first := strings.TrimSpace(strings.Split(fwd, ",")[0]); first != "" {
			host = first
		}
	}
	return host
}
