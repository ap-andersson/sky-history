package links

import (
	"fmt"
	"strings"
	"time"
)

// Generator creates external links to tar1090 instances for viewing aircraft.
type Generator struct {
	urls []string
}

func NewGenerator(urls []string) *Generator {
	// Ensure URLs don't have trailing slashes
	cleaned := make([]string, len(urls))
	for i, u := range urls {
		cleaned[i] = strings.TrimRight(u, "/")
	}
	return &Generator{urls: cleaned}
}

// ExternalLink represents a link to a tar1090 instance.
type ExternalLink struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

// ForAircraft returns links to view an aircraft on all configured tar1090 instances.
func (g *Generator) ForAircraft(icao string) []ExternalLink {
	var links []ExternalLink
	for _, u := range g.urls {
		links = append(links, ExternalLink{
			Name: extractName(u),
			URL:  fmt.Sprintf("%s/?icao=%s", u, strings.ToLower(icao)),
		})
	}
	return links
}

// ForAircraftDate returns links to view an aircraft's trace on a specific date.
func (g *Generator) ForAircraftDate(icao string, date time.Time) []ExternalLink {
	dateStr := date.Format("2006-01-02")
	var links []ExternalLink
	for _, u := range g.urls {
		links = append(links, ExternalLink{
			Name: extractName(u),
			URL:  fmt.Sprintf("%s/?icao=%s&showTrace=%s&trackLabels", u, strings.ToLower(icao), dateStr),
		})
	}
	return links
}

// ForFlight returns links to view a specific flight's trace on tar1090.
func (g *Generator) ForFlight(icao string, date time.Time) []ExternalLink {
	dateStr := date.Format("2006-01-02")
	var links []ExternalLink
	for _, u := range g.urls {
		links = append(links, ExternalLink{
			Name: extractName(u),
			URL:  fmt.Sprintf("%s/?icao=%s&showTrace=%s&trackLabels", u, strings.ToLower(icao), dateStr),
		})
	}
	return links
}

// extractName derives a short display name from a URL.
func extractName(url string) string {
	// Remove protocol
	name := url
	if i := strings.Index(name, "://"); i >= 0 {
		name = name[i+3:]
	}
	// Remove port
	if i := strings.Index(name, ":"); i >= 0 {
		name = name[:i]
	}
	// Remove path
	if i := strings.Index(name, "/"); i >= 0 {
		name = name[:i]
	}
	return name
}
