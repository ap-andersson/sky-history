package collect

import (
	"context"
	"log"
	"math"
	"strings"
	"time"

	"github.com/sky-history/collector/db"
	"github.com/sky-history/shared/feedcheck"
)

// maxBackoff caps how far a failing feeder is backed off. A receiver that is
// simply down should not be hammered, but it must recover promptly once it
// returns, so the ceiling stays low.
const maxBackoff = 5 * time.Minute

// Poller polls one feeder on an interval and reports what it sees.
type Poller struct {
	feeder   db.Feeder
	client   *feedcheck.Client
	interval time.Duration

	sightings chan<- []Sighting
	results   chan<- db.PollResult
}

// Run polls until the context is cancelled.
func (p *Poller) Run(ctx context.Context) {
	log.Printf("Feeder %q: polling every %s", p.feeder.Name, p.interval)

	failures := 0
	for {
		snap, err := p.client.Fetch(ctx, p.feeder.URL)
		if ctx.Err() != nil {
			return
		}

		result := db.PollResult{FeederID: p.feeder.ID, At: time.Now().UTC()}
		if err != nil {
			failures++
			result.Err = err.Error()
			if failures == 1 || failures%10 == 0 {
				log.Printf("Feeder %q: poll failed (%d in a row): %v", p.feeder.Name, failures, err)
			}
		} else {
			if failures > 0 {
				log.Printf("Feeder %q: recovered after %d failed poll(s)", p.feeder.Name, failures)
			}
			failures = 0
			if sightings := p.convert(snap); len(sightings) > 0 {
				select {
				case p.sightings <- sightings:
				case <-ctx.Done():
					return
				}
			}
		}

		select {
		case p.results <- result:
		case <-ctx.Done():
			return
		}

		select {
		case <-time.After(p.wait(failures)):
		case <-ctx.Done():
			return
		}
	}
}

// wait is the delay before the next poll, backing off exponentially while a
// feeder is failing.
func (p *Poller) wait(failures int) time.Duration {
	if failures == 0 {
		return p.interval
	}
	backoff := time.Duration(math.Pow(2, math.Min(float64(failures), 10))) * p.interval
	if backoff > maxBackoff {
		backoff = maxBackoff
	}
	return backoff
}

// convert turns a snapshot into sightings, discarding everything Sky-History
// does not store.
func (p *Poller) convert(snap *feedcheck.Snapshot) []Sighting {
	out := make([]Sighting, 0, len(snap.Aircraft))

	for _, e := range snap.Aircraft {
		if !feedcheck.IsUsable(e) {
			continue
		}

		callsign := strings.ToUpper(strings.TrimSpace(e.Flight))
		if !isValidCallsign(callsign) {
			continue
		}

		// Timestamps come from our own clock minus the age the feeder reports,
		// never from the feeder's "now". A crowdsourced receiver with a wrong
		// clock would otherwise file flights under the wrong day, or the
		// future, and there is no way to tell a wrong clock from a right one.
		age := time.Duration(*e.Seen * float64(time.Second))
		if age < 0 || age > 24*time.Hour {
			continue
		}
		at := snap.ReceivedAt.Add(-age)

		out = append(out, Sighting{
			FeederID:     p.feeder.ID,
			ICAO:         strings.ToUpper(strings.TrimSpace(e.Hex)),
			Callsign:     callsign,
			Registration: strings.TrimSpace(e.Reg),
			TypeCode:     strings.TrimSpace(e.TypeCode),
			Description:  strings.TrimSpace(e.Desc),
			At:           at,
		})
	}

	return out
}

// isValidCallsign checks whether a callsign looks like a real one.
//
// Deliberately the same rules as the trace parser's isValidCallsign, so a
// callsign accepted from a live feeder is one the archive would also have
// accepted, and a day does not change shape when the archive replaces it.
func isValidCallsign(cs string) bool {
	if len(cs) < 2 {
		return false
	}
	if cs[0] == '.' {
		return false
	}
	hasAlnum := false
	allSame := true
	for i, c := range cs {
		if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') {
			hasAlnum = true
		}
		if i > 0 && byte(c) != cs[0] {
			allSame = false
		}
	}
	if !hasAlnum {
		return false
	}
	return !allSame
}

// NewPoller builds a poller for one feeder.
func NewPoller(
	feeder db.Feeder,
	client *feedcheck.Client,
	interval time.Duration,
	sightings chan<- []Sighting,
	results chan<- db.PollResult,
) *Poller {
	return &Poller{
		feeder:    feeder,
		client:    client,
		interval:  interval,
		sightings: sightings,
		results:   results,
	}
}
