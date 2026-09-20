// Package collect turns a stream of aircraft.json snapshots into flight
// segments matching what Sky-History stores for archive data.
package collect

import (
	"strings"
	"time"

	"github.com/sky-history/collector/db"
)

// Sighting is one aircraft seen by one feeder at one moment.
type Sighting struct {
	FeederID     int
	ICAO         string
	Callsign     string
	Registration string
	TypeCode     string
	Description  string

	// When the feeder last heard from this aircraft, computed as the local
	// time the response arrived minus the entry's "seen" age. The feeder's own
	// clock is deliberately not trusted.
	At time.Time
}

// segment is a flight being accumulated in memory.
type segment struct {
	db.Segment
	closed bool
}

// Aggregator folds sightings from every feeder into one segment per aircraft.
//
// Keyed by ICAO rather than by ICAO and callsign, because an aircraft has one
// current callsign and a change of callsign ends the segment -- the same rule
// the trace parser applies to archive data. Two feeders seeing the same
// aircraft therefore extend one segment rather than creating two, which is the
// entire point of accepting more than one feeder.
//
// Not safe for concurrent use: a single goroutine owns it and feeders reach it
// through a channel, which keeps the merge free of locks.
type Aggregator struct {
	open       map[string]*segment
	closed     []db.Segment
	segmentGap time.Duration
}

func NewAggregator(segmentGap time.Duration) *Aggregator {
	return &Aggregator{
		open:       make(map[string]*segment),
		segmentGap: segmentGap,
	}
}

// Observe folds one sighting into the current state.
func (a *Aggregator) Observe(s Sighting) {
	if s.ICAO == "" || s.Callsign == "" {
		return
	}

	cur, ok := a.open[s.ICAO]
	if !ok {
		a.open[s.ICAO] = a.newSegment(s)
		return
	}

	// A sighting older than what we already have carries no news. This also
	// keeps a lagging feeder from dragging a segment backwards or, worse,
	// flipping the callsign back to a stale value.
	if !s.At.After(cur.LastSeen) {
		a.enrich(cur, s)
		return
	}

	if !strings.EqualFold(cur.Callsign, s.Callsign) {
		a.closeSegment(s.ICAO, cur.LastSeen)
		a.open[s.ICAO] = a.newSegment(s)
		return
	}

	// A segment belongs to the UTC date it started on, because that is what
	// flights.date means. An aircraft flying through midnight becomes two
	// rows, exactly as the archive would record it.
	if utcDate(s.At) != utcDate(cur.FirstSeen) {
		a.closeSegment(s.ICAO, endOfDay(cur.FirstSeen))
		a.open[s.ICAO] = a.newSegment(s)
		return
	}

	cur.LastSeen = s.At
	a.enrich(cur, s)
}

// Sweep closes segments that have gone quiet for longer than the gap.
func (a *Aggregator) Sweep(now time.Time) {
	for icao, seg := range a.open {
		if now.Sub(seg.LastSeen) > a.segmentGap {
			a.closeSegment(icao, seg.LastSeen)
		}
	}
}

// CloseAll ends every open segment, for a clean shutdown.
func (a *Aggregator) CloseAll() {
	for icao, seg := range a.open {
		a.closeSegment(icao, seg.LastSeen)
	}
}

// Drain returns everything that should be written now: segments closed since
// the last drain, plus a snapshot of the open ones so an aircraft still in the
// air is searchable. The closed set is emptied; open segments stay.
func (a *Aggregator) Drain() []db.Segment {
	out := make([]db.Segment, 0, len(a.closed)+len(a.open))
	out = append(out, a.closed...)
	a.closed = a.closed[:0]

	for _, seg := range a.open {
		out = append(out, seg.Segment)
	}
	return out
}

// OpenCount reports how many aircraft are currently being tracked.
func (a *Aggregator) OpenCount() int { return len(a.open) }

func (a *Aggregator) newSegment(s Sighting) *segment {
	return &segment{
		Segment: db.Segment{
			ICAO:         s.ICAO,
			Callsign:     s.Callsign,
			Registration: s.Registration,
			TypeCode:     s.TypeCode,
			Description:  s.Description,
			Date:         time.Date(s.At.Year(), s.At.Month(), s.At.Day(), 0, 0, 0, 0, time.UTC),
			FirstSeen:    s.At,
			LastSeen:     s.At,
			FeederID:     s.FeederID,
		},
	}
}

// enrich fills in identity fields a feeder may not have had earlier. Only
// empty fields are filled: the first feeder to name an aircraft keeps its
// answer, so a later feeder disagreeing cannot rewrite it mid-flight.
func (a *Aggregator) enrich(seg *segment, s Sighting) {
	if seg.Registration == "" {
		seg.Registration = s.Registration
	}
	if seg.TypeCode == "" {
		seg.TypeCode = s.TypeCode
	}
	if seg.Description == "" {
		seg.Description = s.Description
	}
}

func (a *Aggregator) closeSegment(icao string, lastSeen time.Time) {
	seg, ok := a.open[icao]
	if !ok {
		return
	}
	seg.LastSeen = lastSeen
	a.closed = append(a.closed, seg.Segment)
	delete(a.open, icao)
}

func utcDate(t time.Time) string {
	return t.UTC().Format("2006-01-02")
}

// endOfDay is the last instant of the UTC day a time falls in, used to clamp a
// segment that ran into the next day.
func endOfDay(t time.Time) time.Time {
	u := t.UTC()
	return time.Date(u.Year(), u.Month(), u.Day(), 23, 59, 59, 999999000, time.UTC)
}
