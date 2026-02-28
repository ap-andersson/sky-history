package github

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// RateLimitError is returned when GitHub responds with 403 or 429 due to rate limiting.
type RateLimitError struct {
	RetryAfter time.Duration
	ResetAt    time.Time
	Status     int
	Message    string
}

func (e *RateLimitError) Error() string {
	if !e.ResetAt.IsZero() {
		return fmt.Sprintf("GitHub rate limited (HTTP %d): %s — retry after %s (resets at %s)",
			e.Status, e.Message, e.RetryAfter.Round(time.Second), e.ResetAt.Format(time.RFC3339))
	}
	return fmt.Sprintf("GitHub rate limited (HTTP %d): %s — retry after %s",
		e.Status, e.Message, e.RetryAfter.Round(time.Second))
}

// Release represents a GitHub release with its assets.
type Release struct {
	TagName     string    `json:"tag_name"`
	Name        string    `json:"name"`
	PublishedAt time.Time `json:"published_at"`
	Assets      []Asset   `json:"assets"`
}

// Asset represents a downloadable file from a GitHub release.
type Asset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
}

// ReleaseInfo holds parsed release information.
type ReleaseInfo struct {
	Tag       string
	Date      time.Time
	AssetURLs []string // ordered: .tar.aa, .tar.ab, etc.
}

// Client interacts with the GitHub Releases API.
type Client struct {
	httpClient *http.Client
	token      string
	repo       string
}

// NewClient creates a new GitHub API client.
func NewClient(repo, token string) *Client {
	return &Client{
		httpClient: &http.Client{Timeout: 30 * time.Second},
		token:      token,
		repo:       repo,
	}
}

// tagPattern matches release tags like "v2026.02.24-planes-readsb-prod-0"
var tagPattern = regexp.MustCompile(`^v(\d{4})\.(\d{2})\.(\d{2})-planes-readsb-prod-\d+$`)

// FetchNewReleases returns prod releases that haven't been processed yet.
// If since is non-zero, only releases on or after that date are returned,
// and pagination stops once a page of prod releases are all older than the cutoff.
// isProcessed is a function that checks if a tag has already been ingested.
func (c *Client) FetchNewReleases(ctx context.Context, since time.Time, isProcessed func(string) (bool, error)) ([]ReleaseInfo, error) {
	var allReleases []ReleaseInfo
	page := 1
	hasCutoff := !since.IsZero()

	for {
		releases, err := c.fetchReleasePage(ctx, page)
		if err != nil {
			return nil, err
		}
		if len(releases) == 0 {
			break
		}

		foundProdRelease := false
		allProdOlderThanCutoff := true
		for _, r := range releases {
			// Only process prod releases (skip mlatonly, staging)
			if !tagPattern.MatchString(r.TagName) {
				continue
			}

			info, err := parseRelease(r)
			if err != nil {
				log.Printf("Skipping release %s: %v", r.TagName, err)
				continue
			}

			foundProdRelease = true

			// Skip releases older than the cutoff date
			if hasCutoff && info.Date.Before(since) {
				continue
			}
			allProdOlderThanCutoff = false

			// Check if already processed
			processed, err := isProcessed(r.TagName)
			if err != nil {
				return nil, fmt.Errorf("check if processed %s: %w", r.TagName, err)
			}
			if processed {
				continue
			}

			log.Printf("  Found unprocessed release: %s (date: %s)", r.TagName, info.Date.Format("2006-01-02"))
			allReleases = append(allReleases, info)
		}

		// Only stop paginating when we've seen prod releases and they're all older than cutoff.
		// If no prod releases appeared on this page, keep going — they may be on later pages.
		if hasCutoff && foundProdRelease && allProdOlderThanCutoff {
			break
		}

		page++

		// Safety limit to prevent infinite pagination
		if page > 50 {
			break
		}
	}

	// Sort oldest first so we process chronologically
	sort.Slice(allReleases, func(i, j int) bool {
		return allReleases[i].Date.Before(allReleases[j].Date)
	})

	return allReleases, nil
}

// FetchRecentReleases fetches only the first few pages of releases (for polling).
func (c *Client) FetchRecentReleases(ctx context.Context, maxPages int, isProcessed func(string) (bool, error)) ([]ReleaseInfo, error) {
	var allReleases []ReleaseInfo

	for page := 1; page <= maxPages; page++ {
		releases, err := c.fetchReleasePage(ctx, page)
		if err != nil {
			return nil, err
		}
		if len(releases) == 0 {
			break
		}

		allUnprocessed := true
		for _, r := range releases {
			if !tagPattern.MatchString(r.TagName) {
				continue
			}

			processed, err := isProcessed(r.TagName)
			if err != nil {
				return nil, err
			}
			if processed {
				allUnprocessed = false
				continue
			}

			info, err := parseRelease(r)
			if err != nil {
				log.Printf("Skipping release %s: %v", r.TagName, err)
				continue
			}
			allReleases = append(allReleases, info)
		}

		// If all releases on this page were already processed, no need to go further
		if !allUnprocessed {
			break
		}
	}

	sort.Slice(allReleases, func(i, j int) bool {
		return allReleases[i].Date.Before(allReleases[j].Date)
	})

	return allReleases, nil
}

func (c *Client) fetchReleasePage(ctx context.Context, page int) ([]Release, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/releases?page=%d&per_page=30", c.repo, page)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Accept", "application/vnd.github+json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch releases page %d: %w", page, err)
	}
	defer resp.Body.Close()

	// Log rate limit status when getting low
	if remaining := resp.Header.Get("X-RateLimit-Remaining"); remaining != "" {
		if n, _ := strconv.Atoi(remaining); n < 20 {
			log.Printf("GitHub API rate limit: %s remaining", remaining)
		}
	}

	// Handle rate limiting (403 with rate limit headers, or 429)
	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests {
		return nil, parseRateLimitError(resp)
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("GitHub API returned %d: %s", resp.StatusCode, string(body))
	}

	var releases []Release
	if err := json.NewDecoder(resp.Body).Decode(&releases); err != nil {
		return nil, fmt.Errorf("decode releases: %w", err)
	}

	return releases, nil
}

// parseRateLimitError extracts wait time from GitHub's rate limit response headers.
func parseRateLimitError(resp *http.Response) *RateLimitError {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))

	rle := &RateLimitError{
		Status:  resp.StatusCode,
		Message: strings.TrimSpace(string(body)),
	}

	// Prefer Retry-After header (seconds)
	if ra := resp.Header.Get("Retry-After"); ra != "" {
		if secs, err := strconv.Atoi(ra); err == nil {
			rle.RetryAfter = time.Duration(secs) * time.Second
			rle.ResetAt = time.Now().UTC().Add(rle.RetryAfter)
			return rle
		}
	}

	// Fall back to X-RateLimit-Reset (unix timestamp)
	if reset := resp.Header.Get("X-RateLimit-Reset"); reset != "" {
		if ts, err := strconv.ParseInt(reset, 10, 64); err == nil {
			resetTime := time.Unix(ts, 0).UTC()
			wait := time.Until(resetTime)
			if wait < 0 {
				wait = 30 * time.Second // clock skew safety
			}
			rle.RetryAfter = wait
			rle.ResetAt = resetTime
			return rle
		}
	}

	// No headers found — use a conservative default
	rle.RetryAfter = 5 * time.Minute
	rle.ResetAt = time.Now().UTC().Add(rle.RetryAfter)
	return rle
}

func parseRelease(r Release) (ReleaseInfo, error) {
	matches := tagPattern.FindStringSubmatch(r.TagName)
	if matches == nil {
		return ReleaseInfo{}, fmt.Errorf("tag does not match pattern: %s", r.TagName)
	}

	dateStr := fmt.Sprintf("%s-%s-%s", matches[1], matches[2], matches[3])
	date, err := time.Parse("2006-01-02", dateStr)
	if err != nil {
		return ReleaseInfo{}, fmt.Errorf("parse date from tag %s: %w", r.TagName, err)
	}

	// Collect asset URLs, sorted by name (to get .tar.aa before .tar.ab)
	var assetURLs []string
	for _, a := range r.Assets {
		name := strings.ToLower(a.Name)
		if strings.Contains(name, ".tar") {
			assetURLs = append(assetURLs, a.BrowserDownloadURL)
		}
	}
	sort.Strings(assetURLs)

	if len(assetURLs) == 0 {
		return ReleaseInfo{}, fmt.Errorf("no tar assets found in release %s", r.TagName)
	}

	return ReleaseInfo{
		Tag:       r.TagName,
		Date:      date,
		AssetURLs: assetURLs,
	}, nil
}
