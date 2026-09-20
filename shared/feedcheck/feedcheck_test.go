package feedcheck

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestIsPublicRejectsUnroutableAddresses(t *testing.T) {
	blocked := []string{
		"127.0.0.1",        // loopback
		"::1",              // loopback v6
		"::ffff:127.0.0.1", // loopback as IPv4-mapped v6
		"10.0.0.5",         // private
		"172.16.0.1",       // private
		"192.168.1.1",      // private
		"169.254.169.254",  // link-local, the cloud metadata address
		"100.64.0.1",       // carrier-grade NAT
		"0.0.0.0",          // unspecified
		"224.0.0.1",        // multicast
		"fd00::1",          // unique local v6
		"fe80::1",          // link-local v6
		"198.18.0.1",       // benchmarking
		"240.0.0.1",        // reserved
		"64:ff9b::7f00:1",  // NAT64 wrapping loopback
	}
	for _, s := range blocked {
		if isPublic(net.ParseIP(s)) {
			t.Errorf("%s was allowed, want blocked", s)
		}
	}

	allowed := []string{"8.8.8.8", "1.1.1.1", "2606:4700:4700::1111", "193.15.1.1"}
	for _, s := range allowed {
		if !isPublic(net.ParseIP(s)) {
			t.Errorf("%s was blocked, want allowed", s)
		}
	}
}

func TestFetchRefusesLoopback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("the guarded client connected to a loopback server")
	}))
	defer srv.Close()

	c := NewClient(5*time.Second, false)
	_, err := c.Fetch(context.Background(), srv.URL)
	if err == nil {
		t.Fatal("want an error fetching a loopback address, got none")
	}
	if !strings.Contains(err.Error(), "not publicly reachable") {
		t.Errorf("error = %q, want it to name the address as unreachable", err)
	}
	// The message must not leak what is listening there.
	if strings.Contains(err.Error(), "127.0.0.1") {
		t.Errorf("error leaks the internal address: %q", err)
	}
}

func TestFetchAllowsLoopbackWhenConfigured(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"now":1789751567.0,"aircraft":[{"hex":"4cafcd","flight":"SAS533  ","r":"EI-SIH","t":"A20N","desc":"AIRBUS A-320neo","seen":0.2}]}`))
	}))
	defer srv.Close()

	c := NewClient(5*time.Second, true)
	snap, err := c.Fetch(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("fetch failed with private addresses allowed: %v", err)
	}
	if len(snap.Aircraft) != 1 || snap.Aircraft[0].Reg != "EI-SIH" {
		t.Errorf("decoded %+v, want the one aircraft", snap.Aircraft)
	}
	if snap.ReceivedAt.IsZero() {
		t.Error("ReceivedAt was not set, so timestamps would fall back to the feeder's clock")
	}
}

func TestFetchRejectsJSONThatIsNotAFeed(t *testing.T) {
	cases := map[string]string{
		"no now field":      `{"aircraft":[]}`,
		"no aircraft array": `{"now":1789751567.0}`,
		"wrong shape":       `{"now":1789751567.0,"aircraft":[{"name":"not an aircraft"}]}`,
		"not json at all":   `<html><body>tar1090</body></html>`,
	}

	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Write([]byte(body))
			}))
			defer srv.Close()

			c := NewClient(5*time.Second, true)
			if _, err := c.Fetch(context.Background(), srv.URL); err == nil {
				t.Errorf("accepted %s as a feed", name)
			}
		})
	}
}

func TestFetchAcceptsAnEmptySky(t *testing.T) {
	// A quiet receiver at 03:00 legitimately sees nothing; rejecting that
	// would turn a working feeder away.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"now":1789751567.0,"aircraft":[]}`))
	}))
	defer srv.Close()

	c := NewClient(5*time.Second, true)
	snap, err := c.Fetch(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("rejected a reachable feeder with an empty sky: %v", err)
	}
	if len(snap.Aircraft) != 0 {
		t.Errorf("want no aircraft, got %d", len(snap.Aircraft))
	}
}

func TestNormalizeURL(t *testing.T) {
	ok := map[string]string{
		"https://adsb.example.se/data/aircraft.json":     "https://adsb.example.se/data/aircraft.json",
		"HTTPS://ADSB.EXAMPLE.SE/data/aircraft.json":     "https://adsb.example.se/data/aircraft.json",
		"https://adsb.example.se:443/data/aircraft.json": "https://adsb.example.se/data/aircraft.json",
		"http://adsb.example.se:80/data/aircraft.json":   "http://adsb.example.se/data/aircraft.json",
		"https://adsb.example.se":                        "https://adsb.example.se/",
		"https://adsb.example.se/x.json#frag":            "https://adsb.example.se/x.json",
		"  https://adsb.example.se/x.json  ":             "https://adsb.example.se/x.json",
	}
	for in, want := range ok {
		got, err := NormalizeURL(in)
		if err != nil {
			t.Errorf("NormalizeURL(%q) errored: %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("NormalizeURL(%q) = %q, want %q", in, got, want)
		}
	}

	bad := []string{
		"",
		"file:///etc/passwd",
		"ftp://example.com/x",
		"gopher://example.com",
		"https://user:pass@example.com/x",
		"not a url at all",
		"//example.com/x",
	}
	for _, in := range bad {
		if got, err := NormalizeURL(in); err == nil {
			t.Errorf("NormalizeURL(%q) = %q, want an error", in, got)
		}
	}
}

func TestIsUsableRejectsNonICAOAndMissingAge(t *testing.T) {
	age := 1.0
	cases := []struct {
		name string
		e    Entry
		want bool
	}{
		{"good", Entry{Hex: "4cafcd", Seen: &age}, true},
		{"uppercase hex", Entry{Hex: "4CAFCD", Seen: &age}, true},
		{"non-ICAO address", Entry{Hex: "~4cafcd", Seen: &age}, false},
		{"short hex", Entry{Hex: "4caf", Seen: &age}, false},
		{"no seen", Entry{Hex: "4cafcd"}, false},
		{"empty", Entry{}, false},
	}
	for _, c := range cases {
		if got := IsUsable(c.e); got != c.want {
			t.Errorf("%s: IsUsable = %v, want %v", c.name, got, c.want)
		}
	}
}
