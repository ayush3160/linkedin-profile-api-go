// Package config loads settings from the environment, or a local .env that is
// never committed.
package config

import (
	"bufio"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config is the service configuration.
type Config struct {
	LiAt       string
	JSessionID string
	APIKeys    []string

	Port                string
	CacheTTL            time.Duration
	CacheMaxEntries     int
	RateLimitRequests   int
	RateLimitWindow     time.Duration
	UpstreamConcurrency int
	UpstreamTimeout     time.Duration
	MinBetweenProfiles  time.Duration
	LogLevel            string
}

// SessionConfigured reports whether both cookies are present.
func (c Config) SessionConfigured() bool {
	return c.LiAt != "" && c.JSessionID != ""
}

// Load reads .env (if present) then the environment, which wins.
func Load(envFile string) Config {
	loadDotEnv(envFile)
	return Config{
		LiAt:                getenv("LI_AT", ""),
		JSessionID:          strings.Trim(getenv("LI_JSESSIONID", ""), `"`),
		APIKeys:             splitKeys(getenv("API_KEYS", "")),
		Port:                getenv("PORT", "8000"),
		CacheTTL:            time.Duration(getenvInt("CACHE_TTL_SECONDS", 3600)) * time.Second,
		CacheMaxEntries:     getenvInt("CACHE_MAX_ENTRIES", 512),
		RateLimitRequests:   getenvInt("RATE_LIMIT_REQUESTS", 20),
		RateLimitWindow:     time.Duration(getenvInt("RATE_LIMIT_WINDOW_SECONDS", 60)) * time.Second,
		UpstreamConcurrency: getenvInt("UPSTREAM_CONCURRENCY", 4),
		UpstreamTimeout:     time.Duration(getenvInt("UPSTREAM_TIMEOUT_SECONDS", 30)) * time.Second,
		MinBetweenProfiles:  time.Duration(getenvInt("MIN_SECONDS_BETWEEN_PROFILES", 2)) * time.Second,
		LogLevel:            getenv("LOG_LEVEL", "info"),
	}
}

// loadDotEnv sets any KEY=VALUE from the file that is not already in the
// environment. Missing file is not an error.
func loadDotEnv(path string) {
	file, err := os.Open(path)
	if err != nil {
		return
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, found := strings.Cut(line, "=")
		if !found {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.Trim(strings.TrimSpace(value), `"'`)
		if _, exists := os.LookupEnv(key); !exists {
			_ = os.Setenv(key, value)
		}
	}
}

func getenv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok && value != "" {
		return value
	}
	return fallback
}

func getenvInt(key string, fallback int) int {
	value, ok := os.LookupEnv(key)
	if !ok || value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		return fallback
	}
	return parsed
}

func splitKeys(value string) []string {
	var keys []string
	for _, key := range strings.Split(value, ",") {
		if trimmed := strings.TrimSpace(key); trimmed != "" {
			keys = append(keys, trimmed)
		}
	}
	return keys
}
