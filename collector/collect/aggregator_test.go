package collect

import (
	"testing"
	"time"
)

func at(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t.UTC()
}

func sighting(feeder int, icao, callsign, when string) Sighting {
	return Sighting{FeederID: feeder, ICAO: icao, Callsign: callsign, At: at(when)}
}

func TestTwoFeedersMergeIntoOneSegment(t *testing.T) {
	a := NewAggregator(15 * time.Minute)

	a.Observe(sighting(1, "4CAFCD", "SAS533", "2026-09-18T10:00:00Z"))
	a.Observe(sighting(2, "4CAFCD", "SAS533", "2026-09-18T10:02:00Z"))
	a.Observe(sighting(1, "4CAFCD", "SAS533", "2026-09-18T10:05:00Z"))

	segs := a.Drain()
	if len(segs) != 1 {
		t.Fatalf("want 1 segment from two feeders seeing one flight, got %d", len(segs))
	}
	if !segs[0].FirstSeen.Equal(at("2026-09-18T10:00:00Z")) {
		t.Errorf("first_seen = %s, want the earliest sighting", segs[0].FirstSeen)
	}
	if !segs[0].LastSeen.Equal(at("2026-09-18T10:05:00Z")) {
		t.Errorf("last_seen = %s, want the latest sighting", segs[0].LastSeen)
	}
	if segs[0].FeederID != 1 {
		t.Errorf("feeder_id = %d, want the feeder that opened the segment", segs[0].FeederID)
	}
}

func TestCallsignChangeSplitsSegment(t *testing.T) {
	a := NewAggregator(15 * time.Minute)

	a.Observe(sighting(1, "4CAFCD", "SAS533", "2026-09-18T10:00:00Z"))
	a.Observe(sighting(1, "4CAFCD", "SAS533", "2026-09-18T10:30:00Z"))
	a.Observe(sighting(1, "4CAFCD", "SAS890", "2026-09-18T11:00:00Z"))

	segs := a.Drain()
	if len(segs) != 2 {
		t.Fatalf("want 2 segments across a callsign change, got %d", len(segs))
	}

	var first, second bool
	for _, s := range segs {
		switch s.Callsign {
		case "SAS533":
			first = true
			if !s.LastSeen.Equal(at("2026-09-18T10:30:00Z")) {
				t.Errorf("SAS533 last_seen = %s, want the final sighting under that callsign", s.LastSeen)
			}
		case "SAS890":
			second = true
			if !s.FirstSeen.Equal(at("2026-09-18T11:00:00Z")) {
				t.Errorf("SAS890 first_seen = %s, want the moment the callsign changed", s.FirstSeen)
			}
		}
	}
	if !first || !second {
		t.Errorf("want one segment per callsign, got %+v", segs)
	}
}

func TestStaleSightingDoesNotFlipCallsignBack(t *testing.T) {
	a := NewAggregator(15 * time.Minute)

	a.Observe(sighting(1, "4CAFCD", "SAS533", "2026-09-18T10:00:00Z"))
	a.Observe(sighting(1, "4CAFCD", "SAS890", "2026-09-18T10:10:00Z"))
	// A lagging feeder still reporting the old callsign, with an older
	// timestamp. It must not reopen the previous segment.
	a.Observe(sighting(2, "4CAFCD", "SAS533", "2026-09-18T10:05:00Z"))

	segs := a.Drain()
	if len(segs) != 2 {
		t.Fatalf("want 2 segments, got %d: %+v", len(segs), segs)
	}
	for _, s := range segs {
		if s.Callsign == "SAS533" && s.FirstSeen.After(at("2026-09-18T10:00:00Z")) {
			t.Errorf("stale sighting reopened the old segment: %+v", s)
		}
	}
}

func TestMidnightSplitsSegmentByUTCDate(t *testing.T) {
	a := NewAggregator(15 * time.Minute)

	a.Observe(sighting(1, "4CAFCD", "SAS533", "2026-09-18T23:50:00Z"))
	a.Observe(sighting(1, "4CAFCD", "SAS533", "2026-09-19T00:10:00Z"))

	segs := a.Drain()
	if len(segs) != 2 {
		t.Fatalf("want a segment per UTC date, got %d", len(segs))
	}

	for _, s := range segs {
		day := s.Date.Format("2006-01-02")
		if day != s.FirstSeen.UTC().Format("2006-01-02") {
			t.Errorf("segment dated %s but starts %s", day, s.FirstSeen)
		}
		if s.LastSeen.UTC().Format("2006-01-02") != day {
			t.Errorf("segment dated %s runs past midnight to %s", day, s.LastSeen)
		}
	}
}

func TestSweepClosesQuietSegments(t *testing.T) {
	a := NewAggregator(15 * time.Minute)
	a.Observe(sighting(1, "4CAFCD", "SAS533", "2026-09-18T10:00:00Z"))

	a.Sweep(at("2026-09-18T10:10:00Z"))
	if a.OpenCount() != 1 {
		t.Errorf("segment closed before the gap elapsed")
	}

	a.Sweep(at("2026-09-18T10:20:00Z"))
	if a.OpenCount() != 0 {
		t.Errorf("segment still open after the gap elapsed")
	}

	segs := a.Drain()
	if len(segs) != 1 || !segs[0].LastSeen.Equal(at("2026-09-18T10:00:00Z")) {
		t.Errorf("closed segment should keep its final sighting time, got %+v", segs)
	}
}

func TestOpenSegmentsAreDrainedRepeatedly(t *testing.T) {
	a := NewAggregator(15 * time.Minute)
	a.Observe(sighting(1, "4CAFCD", "SAS533", "2026-09-18T10:00:00Z"))

	// An aircraft still in the air must be written on every flush, so a search
	// finds it while it is flying, with last_seen advancing.
	if got := len(a.Drain()); got != 1 {
		t.Fatalf("first drain returned %d segments, want 1", got)
	}
	a.Observe(sighting(1, "4CAFCD", "SAS533", "2026-09-18T10:01:00Z"))

	segs := a.Drain()
	if len(segs) != 1 {
		t.Fatalf("second drain returned %d segments, want the still-open one", len(segs))
	}
	if !segs[0].LastSeen.Equal(at("2026-09-18T10:01:00Z")) {
		t.Errorf("last_seen = %s, want it to have advanced", segs[0].LastSeen)
	}
}

func TestEnrichmentKeepsFirstAnswer(t *testing.T) {
	a := NewAggregator(15 * time.Minute)

	s1 := sighting(1, "4CAFCD", "SAS533", "2026-09-18T10:00:00Z")
	s1.Registration = "EI-SIH"
	a.Observe(s1)

	// A second feeder disagreeing must not rewrite the identity mid-flight.
	s2 := sighting(2, "4CAFCD", "SAS533", "2026-09-18T10:01:00Z")
	s2.Registration = "WRONG"
	s2.TypeCode = "A20N"
	a.Observe(s2)

	segs := a.Drain()
	if segs[0].Registration != "EI-SIH" {
		t.Errorf("registration = %q, want the first answer kept", segs[0].Registration)
	}
	if segs[0].TypeCode != "A20N" {
		t.Errorf("type = %q, want an empty field filled in by a later feeder", segs[0].TypeCode)
	}
}
