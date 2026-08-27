package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ayushsharma/linkedin-profile-api/internal/config"
	"github.com/ayushsharma/linkedin-profile-api/internal/linkedin"
	"github.com/ayushsharma/linkedin-profile-api/internal/model"
	"github.com/ayushsharma/linkedin-profile-api/internal/testdata"
)

// stubFetcher stands in for the LinkedIn client so the route can be exercised
// without touching the network.
type stubFetcher struct {
	cards map[string]string
	err   error
	calls int
}

func (s *stubFetcher) FetchProfile(context.Context, string, bool) (map[string]string, error) {
	s.calls++
	if s.err != nil {
		return nil, s.err
	}
	return s.cards, nil
}

func testConfig() config.Config {
	return config.Config{
		LiAt: "x", JSessionID: "ajax:1",
		CacheTTL: time.Minute, CacheMaxEntries: 32,
		RateLimitRequests: 100, RateLimitWindow: time.Minute,
	}
}

func newServer(t *testing.T, cfg config.Config, fetcher Fetcher) http.Handler {
	t.Helper()
	server := New(cfg, fetcher, nil)
	server.sleep = func(time.Duration) {} // no real throttling in tests
	return server.Routes()
}

func get(t *testing.T, handler http.Handler, target string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, target, nil)
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	return recorder
}

const profileQuery = "/profile?url=https://www.linkedin.com/in/" + testdata.Vanity + "/"

func TestHealth(t *testing.T) {
	handler := newServer(t, testConfig(), &stubFetcher{})
	response := get(t, handler, "/health", nil)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d", response.Code)
	}
	var body model.HealthResponse
	decode(t, response, &body)
	if body.Status != "ok" || !body.SessionConfigured {
		t.Errorf("body = %+v", body)
	}
}

func TestProfileEndToEnd(t *testing.T) {
	handler := newServer(t, testConfig(), &stubFetcher{cards: testdata.AllCards()})
	response := get(t, handler, profileQuery, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body)
	}
	var body model.ProfileResponse
	decode(t, response, &body)

	if body.Profile.Name != "Ada Lovelace" {
		t.Errorf("name = %q", body.Profile.Name)
	}
	if body.Profile.Headline != "Mathematician | First computer programmer" {
		t.Errorf("headline = %q", body.Profile.Headline)
	}
	if body.Profile.NetworkDistance != "2nd" {
		t.Errorf("network_distance = %q", body.Profile.NetworkDistance)
	}
	if len(body.Profile.Experience) == 0 || body.Profile.Experience[0].Title != "Head of Analytics" {
		t.Errorf("experience = %+v", body.Profile.Experience)
	}
	if len(body.Profile.Skills) != 2 {
		t.Errorf("skills = %+v", body.Profile.Skills)
	}
	if body.Meta.Cached {
		t.Error("first call should not be cached")
	}
}

func TestSecondCallIsCached(t *testing.T) {
	fetcher := &stubFetcher{cards: testdata.AllCards()}
	handler := newServer(t, testConfig(), fetcher)

	var first, second, third model.ProfileResponse
	decode(t, get(t, handler, profileQuery, nil), &first)
	decode(t, get(t, handler, profileQuery, nil), &second)
	decode(t, get(t, handler, profileQuery+"&refresh=true", nil), &third)

	if first.Meta.Cached || !second.Meta.Cached || third.Meta.Cached {
		t.Errorf("cached flags = %v, %v, %v", first.Meta.Cached, second.Meta.Cached, third.Meta.Cached)
	}
	if fetcher.calls != 2 {
		t.Errorf("upstream calls = %d, want 2 (cache hit in the middle)", fetcher.calls)
	}
}

func TestBareVanityNameIsAccepted(t *testing.T) {
	handler := newServer(t, testConfig(), &stubFetcher{cards: testdata.AllCards()})
	if response := get(t, handler, "/profile?url="+testdata.Vanity, nil); response.Code != http.StatusOK {
		t.Errorf("status = %d, body = %s", response.Code, response.Body)
	}
}

func TestInvalidURL(t *testing.T) {
	handler := newServer(t, testConfig(), &stubFetcher{})
	response := get(t, handler, "/profile?url=https://example.com/in/x", nil)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", response.Code)
	}
	var body model.ErrorResponse
	decode(t, response, &body)
	if body.Code != "invalid_profile_url" {
		t.Errorf("code = %q", body.Code)
	}
}

func TestUpstreamErrorsMapToStatusCodes(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{"expired", &linkedin.Error{Status: 503, Code: "session_expired", Message: "x"}, 503, "session_expired"},
		{"throttled", &linkedin.Error{Status: 503, Code: "upstream_rate_limited", Message: "x"}, 503, "upstream_rate_limited"},
		{"missing", &linkedin.Error{Status: 404, Code: "profile_not_found", Message: "x"}, 404, "profile_not_found"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			handler := newServer(t, testConfig(), &stubFetcher{err: tc.err})
			response := get(t, handler, profileQuery, nil)
			if response.Code != tc.status {
				t.Fatalf("status = %d, want %d", response.Code, tc.status)
			}
			var body model.ErrorResponse
			decode(t, response, &body)
			if body.Code != tc.code {
				t.Errorf("code = %q, want %q", body.Code, tc.code)
			}
		})
	}
}

func TestNoSessionConfiguredIs503(t *testing.T) {
	cfg := testConfig()
	cfg.LiAt, cfg.JSessionID = "", ""
	handler := newServer(t, cfg, &stubFetcher{})
	response := get(t, handler, profileQuery, nil)
	if response.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d", response.Code)
	}
}

func TestRateLimit(t *testing.T) {
	cfg := testConfig()
	cfg.RateLimitRequests = 2
	handler := newServer(t, cfg, &stubFetcher{cards: testdata.AllCards()})

	for i := range 2 {
		if response := get(t, handler, profileQuery, nil); response.Code != http.StatusOK {
			t.Fatalf("request %d: status = %d", i, response.Code)
		}
	}
	response := get(t, handler, profileQuery, nil)
	if response.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", response.Code)
	}
	if response.Header().Get("Retry-After") == "" {
		t.Error("429 should carry Retry-After")
	}
}

func TestAPIKeyEnforcedWhenConfigured(t *testing.T) {
	cfg := testConfig()
	cfg.APIKeys = []string{"secret-key"}
	handler := newServer(t, cfg, &stubFetcher{cards: testdata.AllCards()})

	if response := get(t, handler, profileQuery, nil); response.Code != http.StatusUnauthorized {
		t.Errorf("no key: status = %d, want 401", response.Code)
	}
	if response := get(t, handler, profileQuery, map[string]string{"X-API-Key": "wrong"}); response.Code != http.StatusUnauthorized {
		t.Errorf("wrong key: status = %d, want 401", response.Code)
	}
	if response := get(t, handler, profileQuery, map[string]string{"X-API-Key": "secret-key"}); response.Code != http.StatusOK {
		t.Errorf("right key: status = %d, body = %s", response.Code, response.Body)
	}
}

func TestRawExposesTheOutline(t *testing.T) {
	handler := newServer(t, testConfig(), &stubFetcher{cards: testdata.AllCards()})
	response := get(t, handler, "/raw?url="+testdata.Vanity, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d", response.Code)
	}
	var body struct {
		VanityName string                      `json:"vanity_name"`
		Cards      map[string][]map[string]any `json:"cards"`
	}
	decode(t, response, &body)
	if body.VanityName != testdata.Vanity {
		t.Errorf("vanity_name = %q", body.VanityName)
	}
	if len(body.Cards["shell"]) == 0 {
		t.Error("shell outline should not be empty")
	}
}

func TestOpenAPIAndIndexAreServed(t *testing.T) {
	handler := newServer(t, testConfig(), &stubFetcher{})
	spec := get(t, handler, "/openapi.json", nil)
	if spec.Code != http.StatusOK {
		t.Fatalf("openapi status = %d", spec.Code)
	}
	var parsed map[string]any
	decode(t, spec, &parsed)
	if parsed["openapi"] == nil {
		t.Error("openapi.json is not a spec")
	}
	if index := get(t, handler, "/", nil); index.Code != http.StatusOK {
		t.Errorf("index status = %d", index.Code)
	}
	if missing := get(t, handler, "/nope", nil); missing.Code != http.StatusNotFound {
		t.Errorf("unknown path status = %d, want 404", missing.Code)
	}
}

func decode(t *testing.T, response *httptest.ResponseRecorder, into any) {
	t.Helper()
	if err := json.Unmarshal(response.Body.Bytes(), into); err != nil {
		t.Fatalf("decoding %s: %v", response.Body.String(), err)
	}
}
