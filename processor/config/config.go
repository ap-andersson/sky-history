package config

import (
	"log"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/joho/godotenv"
)

func init() {
	// godotenv.Load does NOT overwrite existing env vars,
	// so real env vars (e.g. from Docker) always take priority.
	envFile := findEnvFile()
	if envFile != "" {
		if err := godotenv.Load(envFile); err != nil {
			log.Printf("Note: could not load %s: %v", envFile, err)
		} else {
			log.Printf("Loaded config from %s", envFile)
		}
	}
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
	pollInterval, _ := time.ParseDuration(getEnv("POLL_INTERVAL", "1h"))
	backfillDays, _ := strconv.Atoi(getEnv("BACKFILL_DAYS", "0"))
	parseWorkers, _ := strconv.Atoi(getEnv("PARSE_WORKERS", "4"))
	if parseWorkers < 1 {
		parseWorkers = 1
	}

	kd := getEnv("KEEP_DOWNLOADS", "false")
	keepDownloads := kd == "true" || kd == "1" || kd == "yes"

	// Default temp dir: use OS temp on Windows, /tmp/sky-history in containers
	defaultTemp := "/tmp/sky-history"
	if os.TempDir() != "/tmp" {
		defaultTemp = filepath.Join(os.TempDir(), "sky-history")
	}

	return Config{
		DatabaseURL:   getEnv("DATABASE_URL", "postgres://skyhistory:skyhistory@skyhistory-db:5432/skyhistory?sslmode=disable"),
		GitHubToken:   getEnv("GITHUB_TOKEN", ""),
		GitHubRepo:    getEnv("GITHUB_REPO", "adsblol/globe_history_2026"),
		PollInterval:  pollInterval,
		BackfillDays:  backfillDays,
		ParseWorkers:  parseWorkers,
		TempDir:       getEnv("TEMP_DIR", defaultTemp),
		KeepDownloads: keepDownloads,
	}
}

func getEnv(key, fallback string) string {
	if val, ok := os.LookupEnv(key); ok {
		return val
	}
	return fallback
}

// findEnvFile walks upward from the current directory looking for a .env file,
// so both processor/ and api/ find the single .env at the project root.
// The CONFIG_FILE env var can override this search.
func findEnvFile() string {
	if f := os.Getenv("CONFIG_FILE"); f != "" {
		return f
	}
	dir, err := os.Getwd()
	if err != nil {
		return ""
	}
	for {
		candidate := filepath.Join(dir, ".env")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}
