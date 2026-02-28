package parser

import (
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/sky-history/processor/models"
)

// traceJSON represents the top-level structure of a readsb trace JSON file.
type traceJSON struct {
	ICAO      string          `json:"icao"`
	R         string          `json:"r"`    // registration
	T         string          `json:"t"`    // type code
	Desc      string          `json:"desc"` // long type name
	Timestamp float64         `json:"timestamp"`
	Trace     [][]interface{} `json:"trace"`
}

// traceDetail represents the aircraft detail object at trace[i][8].
type traceDetail struct {
	Flight string `json:"flight"`
}

// ParseDirectory walks a directory tree looking for trace JSON files and
// parses them concurrently using the specified number of workers.
func ParseDirectory(dir string, date time.Time, workers int) ([]models.ParsedAircraft, error) {
	// Find all trace files
	var traceFiles []string
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Skip errors, continue walking
		}
		if info.IsDir() {
			return nil
		}
		name := info.Name()
		// Match trace_full_*.json or trace_full_*.json.gz
		if strings.HasPrefix(name, "trace_full_") && strings.Contains(name, ".json") {
			traceFiles = append(traceFiles, path)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk directory: %w", err)
	}

	if len(traceFiles) == 0 {
		return nil, fmt.Errorf("no trace files found in %s", dir)
	}

	log.Printf("  Found %d trace files to parse", len(traceFiles))

	// Process files concurrently
	type result struct {
		aircraft *models.ParsedAircraft
		err      error
	}

	fileCh := make(chan string, len(traceFiles))
	resultCh := make(chan result, len(traceFiles))

	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for path := range fileCh {
				a, err := parseTraceFile(path, date)
				resultCh <- result{aircraft: a, err: err}
			}
		}()
	}

	// Feed files to workers
	for _, f := range traceFiles {
		fileCh <- f
	}
	close(fileCh)

	// Wait for all workers to finish, then close results channel
	go func() {
		wg.Wait()
		close(resultCh)
	}()

	// Collect results
	var aircraft []models.ParsedAircraft
	errCount := 0
	for r := range resultCh {
		if r.err != nil {
			errCount++
			if errCount <= 10 {
				log.Printf("  Warning: parse error: %v", r.err)
			}
			continue
		}
		if r.aircraft != nil && (len(r.aircraft.Flights) > 0 || r.aircraft.ICAO != "") {
			aircraft = append(aircraft, *r.aircraft)
		}
	}

	if errCount > 0 {
		log.Printf("  %d files had parse errors (out of %d total)", errCount, len(traceFiles))
	}

	return aircraft, nil
}

func parseTraceFile(path string, date time.Time) (*models.ParsedAircraft, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", filepath.Base(path), err)
	}
	defer f.Close()

	var reader io.Reader = f

	// Detect gzip by magic bytes (0x1f 0x8b) — some files are gzip-compressed
	// but use a plain .json extension.
	header := make([]byte, 2)
	if _, err := io.ReadFull(f, header); err != nil {
		return nil, fmt.Errorf("read header %s: %w", filepath.Base(path), err)
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return nil, fmt.Errorf("seek %s: %w", filepath.Base(path), err)
	}

	if header[0] == 0x1f && header[1] == 0x8b {
		gz, err := gzip.NewReader(f)
		if err != nil {
			return nil, fmt.Errorf("gzip %s: %w", filepath.Base(path), err)
		}
		defer gz.Close()
		reader = gz
	}

	var trace traceJSON
	if err := json.NewDecoder(reader).Decode(&trace); err != nil {
		return nil, fmt.Errorf("decode %s: %w", filepath.Base(path), err)
	}

	// Clean ICAO: remove leading ~ for non-ICAO addresses
	icao := strings.TrimPrefix(trace.ICAO, "~")
	if icao == "" {
		return nil, nil
	}

	// Skip non-ICAO addresses (they start with ~ in the file name or icao field)
	if strings.HasPrefix(trace.ICAO, "~") {
		return nil, nil
	}

	aircraft := &models.ParsedAircraft{
		ICAO:         strings.ToUpper(icao),
		Registration: strings.TrimSpace(trace.R),
		TypeCode:     strings.TrimSpace(trace.T),
		Description:  strings.TrimSpace(trace.Desc),
	}

	// Extract flight segments from trace data
	aircraft.Flights = extractFlights(trace, date)

	return aircraft, nil
}

// extractFlights processes the trace array and extracts flight segments.
// A flight segment is a continuous period where an aircraft uses the same callsign.
// Segments are split when:
//   - The callsign changes
//   - A new leg is detected (flags & 2)
func extractFlights(trace traceJSON, date time.Time) []models.ParsedFlight {
	if len(trace.Trace) == 0 {
		return nil
	}

	baseTime := time.Unix(int64(trace.Timestamp), int64((trace.Timestamp-float64(int64(trace.Timestamp)))*1e9)).UTC()

	type segment struct {
		callsign  string
		firstSeen time.Time
		lastSeen  time.Time
	}

	var segments []segment
	var current *segment

	for _, point := range trace.Trace {
		if len(point) < 2 {
			continue
		}

		// point[0] is seconds after base timestamp
		offsetSec, ok := toFloat64(point[0])
		if !ok {
			continue
		}
		pointTime := baseTime.Add(time.Duration(offsetSec * float64(time.Second)))

		// Extract callsign from point[8] (aircraft detail object)
		callsign := ""
		if len(point) > 8 && point[8] != nil {
			callsign = extractCallsign(point[8])
		}

		// Check for new leg flag (flags & 2)
		isNewLeg := false
		if len(point) > 6 {
			if flags, ok := toFloat64(point[6]); ok {
				isNewLeg = int(flags)&2 > 0
			}
		}

		// Skip points with no callsign
		if callsign == "" {
			// If we have an active segment, update its last_seen
			// (aircraft may briefly lose callsign between updates)
			if current != nil {
				current.lastSeen = pointTime
			}
			continue
		}

		callsign = strings.TrimSpace(callsign)

		// Skip invalid/junk callsigns
		if !isValidCallsign(callsign) {
			if current != nil {
				current.lastSeen = pointTime
			}
			continue
		}

		// Start a new segment if:
		// - No current segment
		// - Callsign changed
		// - New leg detected
		if current == nil || current.callsign != callsign || isNewLeg {
			// Save previous segment
			if current != nil && current.callsign != "" {
				segments = append(segments, *current)
			}
			current = &segment{
				callsign:  callsign,
				firstSeen: pointTime,
				lastSeen:  pointTime,
			}
		} else {
			current.lastSeen = pointTime
		}
	}

	// Don't forget the last segment
	if current != nil && current.callsign != "" {
		segments = append(segments, *current)
	}

	// Convert to model
	var flights []models.ParsedFlight
	for _, s := range segments {
		flights = append(flights, models.ParsedFlight{
			Callsign:  s.callsign,
			FirstSeen: s.firstSeen,
			LastSeen:  s.lastSeen,
		})
	}

	return flights
}

// extractCallsign tries to get the "flight" field from a trace detail object.
func extractCallsign(v interface{}) string {
	switch detail := v.(type) {
	case map[string]interface{}:
		if flight, ok := detail["flight"]; ok {
			if s, ok := flight.(string); ok {
				return strings.TrimSpace(s)
			}
		}
	}
	return ""
}

// isValidCallsign checks whether a callsign looks like a real one.
// Rejects empty strings, single characters, all-punctuation, strings starting
// with '.', and strings that contain only repeated identical characters.
func isValidCallsign(cs string) bool {
	if len(cs) < 2 {
		return false
	}
	// Reject if starts with '.' (e.g. ".SE-ROG")
	if cs[0] == '.' {
		return false
	}
	// Must contain at least one alphanumeric character
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
	// Reject if every character is the same (e.g. "@@@@@", "00000")
	if allSame {
		return false
	}
	return true
}

// toFloat64 converts a JSON number to float64.
func toFloat64(v interface{}) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	}
	return 0, false
}
