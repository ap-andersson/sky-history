package config

import (
	"log"
	"os"
	"path/filepath"
	"strings"

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
	DatabaseURL     string
	ListenAddr      string
	UltrafeederURLs []string
}

func Load() Config {
	urls := parseURLs(getEnv("ULTRAFEEDER_URLS", ""))
	// Always include the public instances
	defaults := []string{
		"https://globe.adsb.fi",
		"https://adsb.lol",
		"https://globe.adsbexchange.com",
	}
	urls = append(defaults, urls...)

	return Config{
		DatabaseURL:     getEnv("DATABASE_URL", "postgres://skyhistory:skyhistory@localhost:5432/skyhistory?sslmode=disable"),
		ListenAddr:      getEnv("LISTEN_ADDR", ":8081"),
		UltrafeederURLs: urls,
	}
}

func getEnv(key, fallback string) string {
	if val, ok := os.LookupEnv(key); ok {
		return val
	}
	return fallback
}

// findEnvFile walks upward from the current directory looking for a .env file.
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
