package handlers

import (
	"net/http"
	"testing"
	"time"
)

func TestSlidingWindowLimiterAllowsUpToMax(t *testing.T) {
	l := newSlidingWindowLimiter(time.Minute, 3)

	for i := 0; i < 3; i++ {
		if !l.allow("a") {
			t.Fatalf("request %d: want allowed, got denied", i+1)
		}
	}
	if l.allow("a") {
		t.Error("4th request within the window: want denied, got allowed")
	}
}

func TestSlidingWindowLimiterKeysAreIndependent(t *testing.T) {
	l := newSlidingWindowLimiter(time.Minute, 1)

	if !l.allow("a") {
		t.Fatal("first request for key a: want allowed")
	}
	if !l.allow("b") {
		t.Error("first request for key b: want allowed, a's usage must not count against b")
	}
	if l.allow("a") {
		t.Error("second request for key a: want denied")
	}
}

func TestSlidingWindowLimiterRemaining(t *testing.T) {
	l := newSlidingWindowLimiter(time.Minute, 5)

	if got := l.remaining("a"); got != 5 {
		t.Errorf("remaining before any request = %d, want 5", got)
	}
	l.allow("a")
	l.allow("a")
	if got := l.remaining("a"); got != 3 {
		t.Errorf("remaining after 2 requests = %d, want 3", got)
	}

	for i := 0; i < 5; i++ {
		l.allow("a")
	}
	if got := l.remaining("a"); got != 0 {
		t.Errorf("remaining once over the limit = %d, want 0 (never negative)", got)
	}
}

func TestSlidingWindowLimiterExpiresOldEvents(t *testing.T) {
	l := newSlidingWindowLimiter(10*time.Millisecond, 1)

	if !l.allow("a") {
		t.Fatal("first request: want allowed")
	}
	if l.allow("a") {
		t.Fatal("second request inside the window: want denied")
	}

	time.Sleep(20 * time.Millisecond)

	if !l.allow("a") {
		t.Error("request after the window elapsed: want allowed, the old event should have expired")
	}
}

func TestClientAddrPrefersForwardedFor(t *testing.T) {
	r, err := http.NewRequest("GET", "/", nil)
	if err != nil {
		t.Fatal(err)
	}
	r.RemoteAddr = "10.0.0.1:54321"
	r.Header.Set("X-Forwarded-For", "203.0.113.7, 10.0.0.1")

	// Trusted here exactly as it is in production: this header is only
	// meaningful because nginx is the only thing that can reach this service
	// directly and always sets it (see clientAddr's own comment).
	if got := clientAddr(r); got != "203.0.113.7" {
		t.Errorf("clientAddr = %q, want the first (client) hop of X-Forwarded-For", got)
	}
}

func TestClientAddrFallsBackToRemoteAddr(t *testing.T) {
	r, err := http.NewRequest("GET", "/", nil)
	if err != nil {
		t.Fatal(err)
	}
	r.RemoteAddr = "192.0.2.5:1234"

	if got := clientAddr(r); got != "192.0.2.5" {
		t.Errorf("clientAddr = %q, want the request's own RemoteAddr with the port stripped", got)
	}
}
