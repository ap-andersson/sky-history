// Package config holds the environment plumbing every service shares: finding
// the project's .env file, and reading typed values out of the environment.
//
// The Config structs themselves stay with their services. They have almost
// nothing in common beyond DATABASE_URL -- the processor cares about GitHub
// tokens and parse workers, the API about listen addresses and salts, the
// collector about poll intervals -- so a shared struct would be the union of
// three unrelated sets of knobs, and every service would carry fields that mean
// nothing to it. What was actually duplicated three times is below.
package config

import (
	"log"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/joho/godotenv"
)

// LoadEnvFile finds the project's .env and loads it. Each service's config
// package calls this from its init.
//
// godotenv.Load does NOT overwrite existing env vars, so real env vars (e.g.
// from Docker) always take priority.
func LoadEnvFile() {
	envFile := findEnvFile()
	if envFile == "" {
		return
	}
	if err := godotenv.Load(envFile); err != nil {
		log.Printf("Note: could not load %s: %v", envFile, err)
		return
	}
	log.Printf("Loaded config from %s", envFile)
}

// findEnvFile walks upward from the current directory looking for a .env file,
// so every service finds the single .env at the project root.
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

// Get returns a string setting, or the fallback when it is unset.
func Get(key, fallback string) string {
	if val, ok := os.LookupEnv(key); ok {
		return val
	}
	return fallback
}

// GetInt returns an integer setting. A value that will not parse falls back
// rather than failing: a typo in one knob should not stop a service booting.
func GetInt(key string, fallback int) int {
	raw, ok := os.LookupEnv(key)
	if !ok || raw == "" {
		return fallback
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		log.Printf("Note: %s=%q is not a number, using %d", key, raw, fallback)
		return fallback
	}
	return n
}

// GetBool returns a boolean setting. Only the affirmative spellings count, so
// anything unrecognised reads as false rather than as an error.
func GetBool(key string, fallback bool) bool {
	raw, ok := os.LookupEnv(key)
	if !ok {
		return fallback
	}
	return raw == "true" || raw == "1" || raw == "yes"
}

// GetDuration returns a duration setting such as "30s" or "1h". Values that
// will not parse, and non-positive ones, fall back.
func GetDuration(key string, fallback time.Duration) time.Duration {
	raw, ok := os.LookupEnv(key)
	if !ok || raw == "" {
		return fallback
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		log.Printf("Note: %s=%q is not a positive duration, using %s", key, raw, fallback)
		return fallback
	}
	return d
}
