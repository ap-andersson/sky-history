package handlers

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/sky-history/api/db"
	"github.com/sky-history/api/links"
)

var (
	errInvalidLimit  = errors.New("'limit' must be an integer between 1 and 1000")
	errInvalidOffset = errors.New("'offset' must be a non-negative integer")
)

// Handler holds dependencies for HTTP handlers.
type Handler struct {
	queries *db.Queries
	links   *links.Generator
}

func NewHandler(queries *db.Queries, linkGen *links.Generator) *Handler {
	return &Handler{queries: queries, links: linkGen}
}

// RegisterRoutes sets up all API routes on the given mux.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/health", h.Health)
	mux.HandleFunc("GET /api/stats", h.Stats)
	mux.HandleFunc("GET /api/search", h.Search)
	mux.HandleFunc("GET /api/search/advanced", h.AdvancedSearch)
	mux.HandleFunc("GET /api/aircraft/{icao}", h.Aircraft)
	mux.HandleFunc("GET /api/aircraft/{icao}/flights", h.AircraftFlights)
	mux.HandleFunc("GET /api/flights/date/{date}", h.FlightsByDate)
	mux.HandleFunc("GET /api/failed-dates", h.FailedDates)
}

// jsonResponse writes a JSON response.
func jsonResponse(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		log.Printf("Error encoding JSON response: %v", err)
	}
}

// jsonError writes a JSON error response.
func jsonError(w http.ResponseWriter, status int, message string) {
	jsonResponse(w, status, map[string]string{"error": message})
}

// Health returns a simple health check.
func (h *Handler) Health(w http.ResponseWriter, r *http.Request) {
	jsonResponse(w, http.StatusOK, map[string]string{"status": "ok"})
}

// Stats returns processing statistics.
func (h *Handler) Stats(w http.ResponseWriter, r *http.Request) {
	stats, err := h.queries.GetStats(r.Context())
	if err != nil {
		log.Printf("Error getting stats: %v", err)
		jsonError(w, http.StatusInternalServerError, "failed to get stats")
		return
	}
	jsonResponse(w, http.StatusOK, stats)
}

// Search handles unified search by callsign, ICAO, or registration.
func (h *Handler) Search(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		jsonError(w, http.StatusBadRequest, "query parameter 'q' is required")
		return
	}

	q = strings.ToUpper(q)
	limit, offset, err := parsePagination(r)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}

	type searchResult struct {
		Type    string      `json:"type"`
		Query   string      `json:"query"`
		Total   int         `json:"total"`
		Limit   int         `json:"limit"`
		Offset  int         `json:"offset"`
		Results interface{} `json:"results"`
		Links   interface{} `json:"links,omitempty"`
	}

	// Try as ICAO hex code (6 hex chars)
	if isICAOHex(q) {
		aircraft, err := h.queries.GetAircraft(r.Context(), q)
		if err != nil {
			log.Printf("Error searching aircraft: %v", err)
			jsonError(w, http.StatusInternalServerError, "search failed")
			return
		}

		flights, total, err := h.queries.GetFlightsByICAO(r.Context(), q, limit, offset)
		if err != nil {
			log.Printf("Error getting flights: %v", err)
			jsonError(w, http.StatusInternalServerError, "search failed")
			return
		}

		jsonResponse(w, http.StatusOK, searchResult{
			Type:   "icao",
			Query:  q,
			Total:  total,
			Limit:  limit,
			Offset: offset,
			Results: map[string]interface{}{
				"aircraft": aircraft,
				"flights":  flights,
			},
			Links: h.links.ForAircraft(q),
		})
		return
	}

	// Try as registration (contains dash or digits mixed with letters, not all digits)
	if looksLikeRegistration(q) {
		flights, total, err := h.queries.SearchByRegistration(r.Context(), q, limit, offset)
		if err != nil {
			log.Printf("Error searching by registration: %v", err)
			jsonError(w, http.StatusInternalServerError, "search failed")
			return
		}
		if total > 0 {
			jsonResponse(w, http.StatusOK, searchResult{
				Type:    "registration",
				Query:   q,
				Total:   total,
				Limit:   limit,
				Offset:  offset,
				Results: flights,
			})
			return
		}
	}

	// Default: search as callsign
	flights, total, err := h.queries.SearchByCallsign(r.Context(), q, limit, offset)
	if err != nil {
		log.Printf("Error searching by callsign: %v", err)
		jsonError(w, http.StatusInternalServerError, "search failed")
		return
	}

	jsonResponse(w, http.StatusOK, searchResult{
		Type:    "callsign",
		Query:   q,
		Total:   total,
		Limit:   limit,
		Offset:  offset,
		Results: flights,
	})
}

// Aircraft returns aircraft details and external links.
func (h *Handler) Aircraft(w http.ResponseWriter, r *http.Request) {
	icao := strings.ToUpper(strings.TrimSpace(r.PathValue("icao")))
	if icao == "" {
		jsonError(w, http.StatusBadRequest, "icao parameter is required")
		return
	}

	aircraft, err := h.queries.GetAircraft(r.Context(), icao)
	if err != nil {
		log.Printf("Error getting aircraft: %v", err)
		jsonError(w, http.StatusInternalServerError, "failed to get aircraft")
		return
	}
	if aircraft == nil {
		jsonError(w, http.StatusNotFound, "aircraft not found")
		return
	}

	jsonResponse(w, http.StatusOK, map[string]interface{}{
		"aircraft": aircraft,
		"links":    h.links.ForAircraft(icao),
	})
}

// AircraftFlights returns flights for a specific aircraft.
func (h *Handler) AircraftFlights(w http.ResponseWriter, r *http.Request) {
	icao := strings.ToUpper(strings.TrimSpace(r.PathValue("icao")))
	if icao == "" {
		jsonError(w, http.StatusBadRequest, "icao parameter is required")
		return
	}

	limit, offset, err := parsePagination(r)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}

	flights, total, err := h.queries.GetFlightsByICAO(r.Context(), icao, limit, offset)
	if err != nil {
		log.Printf("Error getting flights: %v", err)
		jsonError(w, http.StatusInternalServerError, "failed to get flights")
		return
	}

	jsonResponse(w, http.StatusOK, map[string]interface{}{
		"icao":    icao,
		"total":   total,
		"limit":   limit,
		"offset":  offset,
		"flights": flights,
		"links":   h.links.ForAircraft(icao),
	})
}

// FlightsByDate returns all flights on a given date.
func (h *Handler) FlightsByDate(w http.ResponseWriter, r *http.Request) {
	dateStr := r.PathValue("date")
	date, err := time.Parse("2006-01-02", dateStr)
	if err != nil {
		jsonError(w, http.StatusBadRequest, "invalid date format, use YYYY-MM-DD")
		return
	}

	limit, offset, err := parsePagination(r)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}

	flights, total, err := h.queries.GetFlightsByDate(r.Context(), date, limit, offset)
	if err != nil {
		log.Printf("Error getting flights by date: %v", err)
		jsonError(w, http.StatusInternalServerError, "failed to get flights")
		return
	}

	jsonResponse(w, http.StatusOK, map[string]interface{}{
		"date":    dateStr,
		"total":   total,
		"limit":   limit,
		"offset":  offset,
		"flights": flights,
	})
}

func parsePagination(r *http.Request) (limit, offset int, err error) {
	limit = 50
	offset = 0

	if v := r.URL.Query().Get("limit"); v != "" {
		n, e := strconv.Atoi(v)
		if e != nil || n < 1 || n > 1000 {
			return 0, 0, errInvalidLimit
		}
		limit = n
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		n, e := strconv.Atoi(v)
		if e != nil || n < 0 {
			return 0, 0, errInvalidOffset
		}
		offset = n
	}
	return
}

func isICAOHex(s string) bool {
	if len(s) != 6 {
		return false
	}
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}

func looksLikeRegistration(s string) bool {
	return strings.Contains(s, "-") || (len(s) >= 2 && len(s) <= 8 && hasLetter(s) && hasDigit(s))
}

func hasLetter(s string) bool {
	for _, c := range s {
		if c >= 'A' && c <= 'Z' {
			return true
		}
	}
	return false
}

func hasDigit(s string) bool {
	for _, c := range s {
		if c >= '0' && c <= '9' {
			return true
		}
	}
	return false
}

// AdvancedSearch handles precise search with combinable filters.
// Query params: icao, callsign, date (YYYY-MM-DD), date_from, date_to, limit, offset.
// Rules:
//   - At least one filter is required
//   - If date or date range is specified, icao or callsign must also be provided
func (h *Handler) AdvancedSearch(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	icao := strings.ToUpper(strings.TrimSpace(q.Get("icao")))
	callsign := strings.ToUpper(strings.TrimSpace(q.Get("callsign")))
	dateStr := strings.TrimSpace(q.Get("date"))
	dateFromStr := strings.TrimSpace(q.Get("date_from"))
	dateToStr := strings.TrimSpace(q.Get("date_to"))

	// Must have at least one filter
	if icao == "" && callsign == "" && dateStr == "" && dateFromStr == "" && dateToStr == "" {
		jsonError(w, http.StatusBadRequest, "at least one filter is required (icao, callsign, date, date_from/date_to)")
		return
	}

	// Parse dates
	var date, dateFrom, dateTo *time.Time
	if dateStr != "" {
		d, err := time.Parse("2006-01-02", dateStr)
		if err != nil {
			jsonError(w, http.StatusBadRequest, "invalid date format, use YYYY-MM-DD")
			return
		}
		date = &d
	}
	if dateFromStr != "" {
		d, err := time.Parse("2006-01-02", dateFromStr)
		if err != nil {
			jsonError(w, http.StatusBadRequest, "invalid date_from format, use YYYY-MM-DD")
			return
		}
		dateFrom = &d
	}
	if dateToStr != "" {
		d, err := time.Parse("2006-01-02", dateToStr)
		if err != nil {
			jsonError(w, http.StatusBadRequest, "invalid date_to format, use YYYY-MM-DD")
			return
		}
		dateTo = &d
	}

	// date and date_from/date_to are mutually exclusive
	if date != nil && (dateFrom != nil || dateTo != nil) {
		jsonError(w, http.StatusBadRequest, "use either 'date' or 'date_from'/'date_to', not both")
		return
	}

	// If any date filter is set, require icao or callsign too
	hasDate := date != nil || dateFrom != nil || dateTo != nil
	if hasDate && icao == "" && callsign == "" {
		jsonError(w, http.StatusBadRequest, "date filters require 'icao' or 'callsign' to be specified")
		return
	}

	// Date range validation
	if dateFrom != nil && dateTo != nil && dateTo.Before(*dateFrom) {
		jsonError(w, http.StatusBadRequest, "date_to must not be before date_from")
		return
	}

	// Limit date range to 1 year
	if dateFrom != nil && dateTo != nil {
		if dateTo.Sub(*dateFrom) > 366*24*time.Hour {
			jsonError(w, http.StatusBadRequest, "date range must not exceed 1 year")
			return
		}
	}

	limit, offset, pErr := parsePagination(r)
	if pErr != nil {
		jsonError(w, http.StatusBadRequest, pErr.Error())
		return
	}

	filter := db.AdvancedFilter{
		ICAO:     icao,
		Callsign: callsign,
		Date:     date,
		DateFrom: dateFrom,
		DateTo:   dateTo,
	}

	flights, total, err := h.queries.AdvancedSearch(r.Context(), filter, limit, offset)
	if err != nil {
		log.Printf("Error in advanced search: %v", err)
		jsonError(w, http.StatusInternalServerError, "search failed")
		return
	}

	result := map[string]interface{}{
		"total":   total,
		"limit":   limit,
		"offset":  offset,
		"filters": map[string]string{},
		"flights": flights,
	}

	filters := result["filters"].(map[string]string)
	if icao != "" {
		filters["icao"] = icao
	}
	if callsign != "" {
		filters["callsign"] = callsign
	}
	if date != nil {
		filters["date"] = date.Format("2006-01-02")
	}
	if dateFrom != nil {
		filters["date_from"] = dateFrom.Format("2006-01-02")
	}
	if dateTo != nil {
		filters["date_to"] = dateTo.Format("2006-01-02")
	}

	if icao != "" {
		if date != nil {
			result["links"] = h.links.ForAircraftDate(icao, *date)
		} else {
			result["links"] = h.links.ForAircraft(icao)
		}
	}

	jsonResponse(w, http.StatusOK, result)
}

// FailedDates returns all dates that permanently failed processing.
func (h *Handler) FailedDates(w http.ResponseWriter, r *http.Request) {
	dates, err := h.queries.GetFailedDates(r.Context())
	if err != nil {
		log.Printf("Error getting failed dates: %v", err)
		jsonError(w, http.StatusInternalServerError, "failed to get failed dates")
		return
	}
	if dates == nil {
		dates = []db.FailedDate{}
	}
	jsonResponse(w, http.StatusOK, map[string]interface{}{
		"failed_dates": dates,
	})
}
