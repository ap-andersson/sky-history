package config

import (
	"time"

	env "github.com/sky-history/shared/config"
)

func init() {
	env.LoadEnvFile()
}

type Config struct {
	DatabaseURL string

	// Master switch for live gap-fill, shared with the API. Off unless
	// explicitly enabled, so a stack that adds this container does not start
	// writing live rows until it is asked to.
	EnableLiveGapFill bool

	// How often each feeder is polled. Sky-History only needs to know that an
	// aircraft was present under a callsign, so this is far coarser than a map
	// would want -- the cost of a poll is the JSON it returns.
	PollInterval time.Duration

	// How long an aircraft may go unseen before its flight segment is closed.
	SegmentGap time.Duration

	// How often open and closed segments are written to the database. Open
	// segments are written too, so an aircraft still in the air is searchable
	// with a last_seen that advances.
	FlushInterval time.Duration

	// How often the enabled-feeder list is re-read, so flipping feeders.enabled
	// by hand takes effect without restarting the container.
	FeederReloadInterval time.Duration

	// Per-request timeout when polling a feeder.
	FetchTimeout time.Duration

	// Allow feeders on private, loopback and link-local addresses. Needed when
	// feeders sit on the same Docker network or LAN, and unsafe wherever the
	// submit form is reachable by the public -- see shared/feedcheck.
	AllowPrivateFeeders bool
}

func Load() Config {
	return Config{
		DatabaseURL:          env.Get("DATABASE_URL", "postgres://skyhistory:skyhistory@skyhistory-db:5432/skyhistory?sslmode=disable"),
		EnableLiveGapFill:    env.GetBool("ENABLE_LIVE_GAPFILL", false),
		PollInterval:         env.GetDuration("COLLECTOR_POLL_INTERVAL", 5*time.Second),
		SegmentGap:           env.GetDuration("COLLECTOR_SEGMENT_GAP", 15*time.Minute),
		FlushInterval:        env.GetDuration("COLLECTOR_FLUSH_INTERVAL", 30*time.Second),
		FeederReloadInterval: env.GetDuration("COLLECTOR_FEEDER_RELOAD", time.Minute),
		FetchTimeout:         env.GetDuration("COLLECTOR_FETCH_TIMEOUT", 15*time.Second),
		AllowPrivateFeeders:  env.GetBool("ALLOW_PRIVATE_FEEDERS", false),
	}
}
