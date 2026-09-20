// Package feedcheck fetches and validates a feeder's aircraft.json.
//
// A feeder URL is supplied by whoever fills in the submit form, which makes
// every fetch here a request to an address an untrusted party chose. Left
// unguarded that is a server-side request forgery primitive: the submitter
// names an address inside the network the service runs in, and the validation
// error handed back tells them what is listening there. Everything in this
// file exists to close that.
//
// This package lives in the shared/ module rather than in either service,
// because both need it and guarding against SSRF is not something to maintain
// two copies of. Each service requires it with a replace directive pointing at
// ../shared, so the service Dockerfiles build from the repository root -- see
// the build context note in shared/README.md.
package feedcheck

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// MaxResponseBytes caps what will be read from a feeder. A busy receiver's
// aircraft.json runs to a few hundred KB; this leaves room for the largest
// plausible feed while refusing to be fed an endless stream.
const MaxResponseBytes = 16 << 20

// ErrBlockedAddress is returned when a URL resolves to an address the fetcher
// refuses to connect to.
var ErrBlockedAddress = errors.New("address is not publicly routable")

var hexRe = regexp.MustCompile(`^[0-9a-f]{6}$`)

// Entry is one aircraft in a feeder's snapshot. Only the fields Sky-History
// stores are decoded; positions, altitudes and the rest are discarded.
type Entry struct {
	Hex      string   `json:"hex"`
	Flight   string   `json:"flight"`
	Reg      string   `json:"r"`
	TypeCode string   `json:"t"`
	Desc     string   `json:"desc"`
	Seen     *float64 `json:"seen"`
}

// Snapshot is one poll of a feeder.
type Snapshot struct {
	Now      float64 `json:"now"`
	Aircraft []Entry `json:"aircraft"`

	// Set by Fetch to the local time the response came back. Timestamps are
	// derived from this minus each entry's Seen, never from the feeder's own
	// Now field: a crowdsourced feeder with a wrong clock would otherwise
	// write flights into next week.
	ReceivedAt time.Time `json:"-"`
}

// Client fetches feeder snapshots over a guarded transport.
type Client struct {
	http *http.Client
}

// NewClient builds a fetcher that refuses non-public addresses.
//
// allowPrivate disables that refusal, which is needed when the feeders are on
// the same Docker network or LAN as the collector. It must only be set where
// the submit form is not reachable by the public.
func NewClient(timeout time.Duration, allowPrivate bool) *Client {
	dialer := &net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}

	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, err
			}

			ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
			if err != nil {
				return nil, err
			}

			// Dial a vetted IP rather than the hostname. Checking the name's
			// resolution and then letting the stack resolve it again would
			// leave a window in which a DNS answer that passed the check is
			// replaced by one that would not have -- the rebinding trick.
			// Connecting to the literal address closes it.
			var lastErr error
			for _, ip := range ips {
				if !allowPrivate && !isPublic(ip.IP) {
					lastErr = fmt.Errorf("%w: %s", ErrBlockedAddress, ip.IP)
					continue
				}
				conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(ip.IP.String(), port))
				if err == nil {
					return conn, nil
				}
				lastErr = err
			}
			if lastErr == nil {
				lastErr = fmt.Errorf("no addresses for %s", host)
			}
			return nil, lastErr
		},
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: timeout,
		DisableKeepAlives:     false,
		MaxIdleConnsPerHost:   2,
	}

	return &Client{
		http: &http.Client{
			Timeout:   timeout,
			Transport: transport,
			// Redirects run back through DialContext, so each hop is vetted
			// the same way. The cap is only there to stop a redirect loop.
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= 3 {
					return errors.New("too many redirects")
				}
				if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
					return fmt.Errorf("refusing redirect to %s", req.URL.Scheme)
				}
				return nil
			},
		},
	}
}

// Fetch retrieves and decodes one snapshot.
func (c *Client) Fetch(ctx context.Context, rawURL string) (*Snapshot, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("User-Agent", "sky-history-collector/1.0 (+https://github.com/ap-andersson/sky-history)")
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, unwrapRequestError(err)
	}
	defer resp.Body.Close()

	received := time.Now().UTC()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("feeder returned HTTP %d", resp.StatusCode)
	}

	// One byte over the cap so a response that exactly fills it is still
	// recognisable as too large rather than being silently truncated.
	body, err := io.ReadAll(io.LimitReader(resp.Body, MaxResponseBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if len(body) > MaxResponseBytes {
		return nil, fmt.Errorf("response larger than %d MB", MaxResponseBytes>>20)
	}

	var snap Snapshot
	if err := json.Unmarshal(body, &snap); err != nil {
		return nil, fmt.Errorf("response is not aircraft.json: %w", err)
	}
	snap.ReceivedAt = received

	if err := validate(&snap); err != nil {
		return nil, err
	}
	return &snap, nil
}

// validate checks that a decoded response is really an aircraft.json feed and
// not merely some JSON document that happened to parse.
func validate(s *Snapshot) error {
	if s.Now == 0 {
		return errors.New("not an aircraft.json feed: no 'now' timestamp")
	}
	if s.Aircraft == nil {
		return errors.New("not an aircraft.json feed: no 'aircraft' array")
	}

	// An empty array is legitimate -- a quiet receiver at 03:00 sees nothing --
	// so it cannot be an error. But if aircraft are listed, at least one has to
	// look like a real entry, which is what separates a feed from arbitrary JSON.
	if len(s.Aircraft) > 0 && ValidEntries(s.Aircraft) == 0 {
		return errors.New("not an aircraft.json feed: no entry has a valid 'hex' and 'seen'")
	}
	return nil
}

// ValidEntries counts entries carrying the fields Sky-History needs.
func ValidEntries(entries []Entry) int {
	n := 0
	for _, e := range entries {
		if IsUsable(e) {
			n++
		}
	}
	return n
}

// IsUsable reports whether an entry can produce a flight row. Non-ICAO
// addresses are rejected here exactly as the trace parser rejects them, so
// live and archive data agree about which aircraft exist at all.
func IsUsable(e Entry) bool {
	if e.Seen == nil {
		return false
	}
	return hexRe.MatchString(strings.ToLower(strings.TrimSpace(e.Hex)))
}

// NormalizeURL canonicalises a submitted feeder URL and rejects anything that
// is not a plain http(s) address. Returning a single spelling per receiver is
// what lets the unique constraint on feeders.url do its job.
func NormalizeURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", errors.New("URL is required")
	}
	if len(raw) > 2000 {
		return "", errors.New("URL is too long")
	}

	u, err := url.Parse(raw)
	if err != nil {
		return "", errors.New("URL could not be parsed")
	}

	u.Scheme = strings.ToLower(u.Scheme)
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", errors.New("URL must start with http:// or https://")
	}
	if u.Host == "" {
		return "", errors.New("URL has no host")
	}
	if u.User != nil {
		return "", errors.New("URL must not contain credentials")
	}

	host := strings.ToLower(u.Hostname())
	if host == "" {
		return "", errors.New("URL has no host")
	}
	port := u.Port()
	if (u.Scheme == "http" && port == "80") || (u.Scheme == "https" && port == "443") {
		port = ""
	}
	if port != "" {
		u.Host = net.JoinHostPort(host, port)
	} else {
		u.Host = host
	}

	// The fragment is never sent anyway; dropping it keeps two spellings of
	// the same endpoint from both being accepted.
	u.Fragment = ""
	if u.Path == "" {
		u.Path = "/"
	}

	return u.String(), nil
}

// isPublic reports whether an address may be connected to. Everything that is
// not plainly routable on the public internet is refused.
func isPublic(ip net.IP) bool {
	if ip == nil {
		return false
	}
	// An IPv4 address expressed as IPv4-mapped IPv6 must be judged by its IPv4
	// value, or ::ffff:127.0.0.1 would slip past the loopback check.
	if v4 := ip.To4(); v4 != nil {
		ip = v4
	}

	if ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsMulticast() || ip.IsInterfaceLocalMulticast() {
		return false
	}

	for _, block := range blockedNets {
		if block.Contains(ip) {
			return false
		}
	}
	return true
}

// Ranges that are not routable on the public internet but that the net
// package's own predicates do not cover.
var blockedNets = func() []*net.IPNet {
	cidrs := []string{
		"100.64.0.0/10",   // carrier-grade NAT
		"192.0.0.0/24",    // IETF protocol assignments
		"192.0.2.0/24",    // TEST-NET-1
		"198.18.0.0/15",   // benchmarking
		"198.51.100.0/24", // TEST-NET-2
		"203.0.113.0/24",  // TEST-NET-3
		"240.0.0.0/4",     // reserved
		"::/128",          // unspecified
		"2001:db8::/32",   // documentation
		"64:ff9b::/96",    // NAT64, reaches IPv4 space
	}
	nets := make([]*net.IPNet, 0, len(cidrs))
	for _, c := range cidrs {
		if _, n, err := net.ParseCIDR(c); err == nil {
			nets = append(nets, n)
		}
	}
	return nets
}()

// unwrapRequestError turns transport failures into something a submitter can
// act on, without echoing back details that would help map a private network.
func unwrapRequestError(err error) error {
	if errors.Is(err, ErrBlockedAddress) {
		return errors.New("that address is not publicly reachable, so it cannot be polled from here")
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return errors.New("the feeder did not respond in time")
	}
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		if errors.Is(urlErr.Err, ErrBlockedAddress) {
			return errors.New("that address is not publicly reachable, so it cannot be polled from here")
		}
		if urlErr.Timeout() {
			return errors.New("the feeder did not respond in time")
		}
		return fmt.Errorf("could not reach the feeder: %v", urlErr.Err)
	}
	return fmt.Errorf("could not reach the feeder: %v", err)
}
