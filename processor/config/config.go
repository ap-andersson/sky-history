package config

import (
	"os"
	"path/filepath"
	"time"

	env "github.com/sky-history/shared/config"
)

func init() {
	env.LoadEnvFile()
}

type Config struct {
	DatabaseURL   string
	GitHubToken   string
	GitHubRepo    string
	PollInterval  time.Duration
	BackfillDays  int
	ParseWorkers  int
	TempDir       string
	KeepDownloads bool
}

func Load() Config {
	parseWorkers := env.GetInt("PARSE_WORKERS", 4)
	if parseWorkers < 1 {
		parseWorkers = 1
	}

	// Default temp dir: use OS temp on Windows, /tmp/sky-history in containers
	defaultTemp := "/tmp/sky-history"
	if os.TempDir() != "/tmp" {
		defaultTemp = filepath.Join(os.TempDir(), "sky-history")
	}

	return Config{
		DatabaseURL:   env.Get("DATABASE_URL", "postgres://skyhistory:skyhistory@skyhistory-db:5432/skyhistory?sslmode=disable"),
		GitHubToken:   env.Get("GITHUB_TOKEN", ""),
		GitHubRepo:    env.Get("GITHUB_REPO", "adsblol/globe_history_2026"),
		PollInterval:  env.GetDuration("POLL_INTERVAL", time.Hour),
		BackfillDays:  env.GetInt("BACKFILL_DAYS", 0),
		ParseWorkers:  parseWorkers,
		TempDir:       env.Get("TEMP_DIR", defaultTemp),
		KeepDownloads: env.GetBool("KEEP_DOWNLOADS", false),
	}
}
