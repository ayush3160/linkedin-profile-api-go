package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadDefaults(t *testing.T) {
	clearEnv(t)
	cfg := Load(filepath.Join(t.TempDir(), "absent.env"))
	if cfg.Port != "8000" {
		t.Errorf("Port = %q", cfg.Port)
	}
	if cfg.CacheTTL != time.Hour {
		t.Errorf("CacheTTL = %v", cfg.CacheTTL)
	}
	if cfg.SessionConfigured() {
		t.Error("no cookies set, SessionConfigured should be false")
	}
}

func TestLoadReadsDotEnvButEnvironmentWins(t *testing.T) {
	clearEnv(t)
	path := filepath.Join(t.TempDir(), ".env")
	contents := "# comment\nLI_AT=from-file\nLI_JSESSIONID=\"ajax:99\"\nPORT=9999\n\nbroken-line\n"
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PORT", "7777") // already set, so the file must not override

	cfg := Load(path)
	if cfg.LiAt != "from-file" {
		t.Errorf("LiAt = %q", cfg.LiAt)
	}
	// Surrounding quotes are stripped so the csrf header matches the cookie.
	if cfg.JSessionID != "ajax:99" {
		t.Errorf("JSessionID = %q", cfg.JSessionID)
	}
	if cfg.Port != "7777" {
		t.Errorf("Port = %q, environment should win over .env", cfg.Port)
	}
	if !cfg.SessionConfigured() {
		t.Error("SessionConfigured should be true")
	}
}

func TestAPIKeysAreSplitAndTrimmed(t *testing.T) {
	clearEnv(t)
	t.Setenv("API_KEYS", " one , two ,, three ")
	cfg := Load(filepath.Join(t.TempDir(), "absent.env"))
	want := []string{"one", "two", "three"}
	if len(cfg.APIKeys) != len(want) {
		t.Fatalf("APIKeys = %v", cfg.APIKeys)
	}
	for i := range want {
		if cfg.APIKeys[i] != want[i] {
			t.Errorf("APIKeys[%d] = %q, want %q", i, cfg.APIKeys[i], want[i])
		}
	}
}

func TestInvalidIntFallsBackToDefault(t *testing.T) {
	clearEnv(t)
	t.Setenv("CACHE_TTL_SECONDS", "not-a-number")
	cfg := Load(filepath.Join(t.TempDir(), "absent.env"))
	if cfg.CacheTTL != time.Hour {
		t.Errorf("CacheTTL = %v, want the 1h default", cfg.CacheTTL)
	}
}

func clearEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"LI_AT", "LI_JSESSIONID", "API_KEYS", "PORT", "CACHE_TTL_SECONDS",
		"CACHE_MAX_ENTRIES", "RATE_LIMIT_REQUESTS", "RATE_LIMIT_WINDOW_SECONDS",
		"UPSTREAM_CONCURRENCY", "UPSTREAM_TIMEOUT_SECONDS",
		"MIN_SECONDS_BETWEEN_PROFILES", "LOG_LEVEL",
	} {
		t.Setenv(key, "")
		os.Unsetenv(key)
	}
}
