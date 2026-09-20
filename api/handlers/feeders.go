package handlers

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/sky-history/api/db"
	"github.com/sky-history/api/models"
	"github.com/sky-history/shared/feedcheck"
)

const (
	// maxSubmitBody is generous for three short strings and small enough that
	// nothing can be streamed at the endpoint.
	maxSubmitBody = 4 << 10

	// Submissions are rate limited twice: a short burst limit held in memory,
	// and a longer one counted in the database so a restart does not reset it.
	submitBurstWindow = time.Minute
	submitBurstMax    = 3
	submitDailyWindow = 24 * time.Hour
	submitDailyMax    = 10
)

// FeederHandler serves the public feeder roster and the submit form.
//
// Submitting causes this service to fetch a URL chosen by whoever filled in
// the form, which is a server-side request forgery primitive unless it is
// guarded. feedcheck does the guarding; everything here assumes a submitter is
// hostile and that the error text it returns is something they can read.
type FeederHandler struct {
	queries *db.Queries
	probe   *feedcheck.Client

	// ipSalt keys the HMAC that turns a submitter's address into the hash
	// stored on the row. Without a salt the hash would be reversible by
	// hashing every address in the space, which is small enough to exhaust.
	ipSalt []byte

	burst *slidingWindowLimiter
}

func NewFeederHandler(queries *db.Queries, probeTimeout time.Duration, allowPrivate bool, ipSalt []byte) *FeederHandler {
	if len(ipSalt) == 0 {
		// A random salt means the hashes do not survive a restart, which costs
		// only the long-window rate limit. Better than a constant everyone
		// running this project would share.
		ipSalt = make([]byte, 32)
		if _, err := rand.Read(ipSalt); err != nil {
			log.Printf("Warning: could not generate an IP salt: %v", err)
		}
		log.Println("SUBMIT_IP_SALT is not set; using a random salt for this run.")
	}

	return &FeederHandler{
		queries: queries,
		probe:   feedcheck.NewClient(probeTimeout, allowPrivate),
		ipSalt:  ipSalt,
		burst:   newSlidingWindowLimiter(submitBurstWindow, submitBurstMax),
	}
}

// ListFeeders returns the public roster. URLs are never included.
func (h *FeederHandler) ListFeeders(w http.ResponseWriter, r *http.Request) {
	feeders, err := h.queries.ListFeeders(r.Context())
	if err != nil {
		log.Printf("Error listing feeders: %v", err)
		jsonError(w, http.StatusInternalServerError, "failed to list feeders")
		return
	}
	if feeders == nil {
		feeders = []models.Feeder{}
	}

	from, to, count, err := h.queries.LiveWindow(r.Context())
	if err != nil {
		log.Printf("Error reading live window: %v", err)
	}

	resp := map[string]interface{}{
		"feeders":           feeders,
		"live_flight_count": count,
	}
	if from != nil && to != nil {
		resp["live_from"] = from
		resp["live_to"] = to
	}
	jsonResponse(w, http.StatusOK, resp)
}

// SubmitFeeder validates a submitted feeder and records it for approval.
func (h *FeederHandler) SubmitFeeder(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxSubmitBody)

	var sub models.FeederSubmission
	if err := json.NewDecoder(r.Body).Decode(&sub); err != nil {
		jsonError(w, http.StatusBadRequest, "could not read the submission")
		return
	}

	name := strings.TrimSpace(sub.Name)
	contact := strings.TrimSpace(sub.Contact)
	if name == "" {
		jsonError(w, http.StatusBadRequest, "a name for the feeder is required")
		return
	}
	if len(name) > 60 {
		jsonError(w, http.StatusBadRequest, "the name must be 60 characters or fewer")
		return
	}
	if len(contact) > 120 {
		jsonError(w, http.StatusBadRequest, "the contact must be 120 characters or fewer")
		return
	}

	url, err := feedcheck.NormalizeURL(sub.URL)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}

	ipHash := h.hashIP(r)

	// Both limits are checked before the probe, so a flood of submissions
	// cannot be turned into a flood of outbound requests.
	if !h.burst.allow(ipHash) {
		jsonError(w, http.StatusTooManyRequests, "too many submissions just now; try again in a minute")
		return
	}
	recent, err := h.queries.RecentSubmissions(r.Context(), ipHash, submitDailyWindow)
	if err != nil {
		log.Printf("Error counting recent submissions: %v", err)
	} else if recent >= submitDailyMax {
		jsonError(w, http.StatusTooManyRequests, "too many submissions from here today")
		return
	}

	exists, err := h.queries.FeederURLExists(r.Context(), url)
	if err != nil {
		log.Printf("Error checking feeder url: %v", err)
		jsonError(w, http.StatusInternalServerError, "could not check the submission")
		return
	}
	if exists {
		jsonError(w, http.StatusConflict, db.ErrDuplicateFeeder.Error())
		return
	}

	// The probe. A failure is reported back and the row is not written: there
	// is nothing to approve if the URL does not serve aircraft.json.
	ctx, cancel := contextWithTimeout(r)
	defer cancel()

	snap, probeErr := h.probe.Fetch(ctx, url)
	if probeErr != nil {
		jsonResponse(w, http.StatusBadRequest, map[string]interface{}{
			"error": probeErr.Error(),
			"hint":  "The URL must serve readsb's aircraft.json, for example https://your-site/data/aircraft.json",
		})
		return
	}

	usable := feedcheck.ValidEntries(snap.Aircraft)
	if err := h.queries.InsertFeeder(r.Context(), db.NewFeeder{
		URL:           url,
		Name:          name,
		Contact:       contact,
		IPHash:        ipHash,
		ValidationOK:  true,
		ProbeAircraft: usable,
	}); err != nil {
		if errors.Is(err, db.ErrDuplicateFeeder) {
			jsonError(w, http.StatusConflict, err.Error())
			return
		}
		log.Printf("Error inserting feeder: %v", err)
		jsonError(w, http.StatusInternalServerError, "could not record the submission")
		return
	}

	log.Printf("Feeder submitted for approval: %q (%d aircraft in view at probe time)", name, usable)

	jsonResponse(w, http.StatusCreated, map[string]interface{}{
		"status":         "pending",
		"name":           name,
		"probe_aircraft": usable,
		"message": "The feeder answered correctly and is now waiting for an administrator " +
			"to approve it. Nothing is polled until then.",
	})
}

// hashIP turns the submitting address into a keyed hash. The address itself is
// never stored or logged, and the hash cannot be reversed without the salt.
func (h *FeederHandler) hashIP(r *http.Request) string {
	mac := hmac.New(sha256.New, h.ipSalt)
	mac.Write([]byte(clientAddr(r)))
	return hex.EncodeToString(mac.Sum(nil))
}
