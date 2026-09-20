package config

import (
	"strings"
	"time"

	env "github.com/sky-history/shared/config"
)

func init() {
	env.LoadEnvFile()
}

type Config struct {
	DatabaseURL     string
	ListenAddr      string
	UltrafeederURLs []string

	// Master switch for the whole live gap-fill feature: the feeder endpoints,
	// the submit form, the Feeders page and the include_live search parameter.
	// Off unless explicitly enabled, so an existing deployment that updates
	// does not suddenly start accepting feeder submissions from the public.
	EnableLiveGapFill bool

	// Salt for the HMAC that turns a submitter's IP into the hash stored on a
	// feeder row. Leaving it unset means the hashes do not survive a restart,
	// which only weakens the long-window submission limit.
	SubmitIPSalt []byte

	// Allow submitted feeders on private and loopback addresses. Turning this
	// on where the submit form is public hands anyone a probe of the internal
	// network -- see the shared/feedcheck package.
	AllowPrivateFeeders bool

	// How long to wait for a submitted feeder to answer.
	ProbeTimeout time.Duration
}

func Load() Config {
	urls := parseURLs(env.Get("ULTRAFEEDER_URLS", ""))
	// Always include the public instances
	defaults := []string{
		"https://globe.adsb.fi",
		"https://adsb.lol",
		"https://globe.adsbexchange.com",
	}
	urls = append(defaults, urls...)

	return Config{
		DatabaseURL:         env.Get("DATABASE_URL", "postgres://skyhistory:skyhistory@localhost:5432/skyhistory?sslmode=disable"),
		ListenAddr:          env.Get("LISTEN_ADDR", ":8081"),
		UltrafeederURLs:     urls,
		EnableLiveGapFill:   env.GetBool("ENABLE_LIVE_GAPFILL", false),
		SubmitIPSalt:        []byte(env.Get("SUBMIT_IP_SALT", "")),
		AllowPrivateFeeders: env.GetBool("ALLOW_PRIVATE_FEEDERS", false),
		ProbeTimeout:        env.GetDuration("FEEDER_PROBE_TIMEOUT", 10*time.Second),
	}
}

func parseURLs(s string) []string {
	if s == "" {
		return nil
	}
	var urls []string
	for _, part := range strings.Split(s, ",") {
		p := strings.TrimSpace(part)
		if p != "" {
			urls = append(urls, p)
		}
	}
	return urls
}
