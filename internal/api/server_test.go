package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ayush3160/linkedin-profile-api-go/internal/config"
	"github.com/ayush3160/linkedin-profile-api-go/internal/linkedin"
	"github.com/ayush3160/linkedin-profile-api-go/internal/model"
	"github.com/ayush3160/linkedin-profile-api-go/internal/testdata"
)

// stubFetcher stands in for the LinkedIn client so the route can be exercised
// without touching the network.
type stubFetcher struct {
	cards  map[string]string
	failed []string
	err    error
	calls  int
}

func (s *stubFetcher) FetchProfile(context.Context, string, bool) (linkedin.FetchResult, error) {
	s.calls++
	if s.err != nil {
		return linkedin.FetchResult{}, s.err
	}
	return linkedin.FetchResult{Cards: s.cards, Failed: s.failed}, nil
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
	cfg.AnonPerDay = 5
	handler := newServer(t, cfg, &stubFetcher{cards: testdata.AllCards()})

	// No header at all is the published-URL case: it works, on the anonymous
	// allowance. Rejecting it would hand a reviewer a 401 and no way to fix it.
	if response := get(t, handler, profileQuery, nil); response.Code != http.StatusOK {
		t.Errorf("no key: status = %d, want 200 on the anonymous allowance", response.Code)
	}
	// A key that is present but wrong is a mistake worth reporting, not a
	// silent downgrade to anonymous.
	if response := get(t, handler, profileQuery, map[string]string{"X-API-Key": "wrong"}); response.Code != http.StatusUnauthorized {
		t.Errorf("wrong key: status = %d, want 401", response.Code)
	}
	if response := get(t, handler, profileQuery, map[string]string{"X-API-Key": "secret-key"}); response.Code != http.StatusOK {
		t.Errorf("right key: status = %d, body = %s", response.Code, response.Body)
	}
}

func TestAnonymousAllowanceRunsOutButAKeyDoesNot(t *testing.T) {
	cfg := testConfig()
	cfg.APIKeys = []string{"secret-key"}
	cfg.AnonPerDay = 2
	fetcher := &stubFetcher{cards: testdata.AllCards()}
	handler := newServer(t, cfg, fetcher)

	// Each call asks for a different profile so the cache does not absorb it.
	for i, vanity := range []string{"alpha", "beta"} {
		if response := get(t, handler, "/profile?url="+vanity, nil); response.Code != http.StatusOK {
			t.Fatalf("anonymous call %d: status = %d", i, response.Code)
		}
	}
	response := get(t, handler, "/profile?url=gamma", nil)
	if response.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429 once the allowance is spent", response.Code)
	}
	var body model.ErrorResponse
	decode(t, response, &body)
	if body.Code != "anonymous_quota_exhausted" {
		t.Errorf("code = %q, want anonymous_quota_exhausted", body.Code)
	}
	// The caller has no way to know a key exists unless the error says so.
	if !strings.Contains(body.Detail, "X-API-Key") {
		t.Errorf("detail = %q, should name the header", body.Detail)
	}
	if response.Header().Get("Retry-After") == "" {
		t.Error("429 should carry Retry-After")
	}
	// A key is not subject to the anonymous allowance.
	if response := get(t, handler, "/profile?url=delta", map[string]string{"X-API-Key": "secret-key"}); response.Code != http.StatusOK {
		t.Errorf("keyed call after the allowance ran out: status = %d", response.Code)
	}
}

func TestCachedRepeatsDoNotSpendTheAnonymousAllowance(t *testing.T) {
	cfg := testConfig()
	cfg.APIKeys = []string{"secret-key"}
	cfg.AnonPerDay = 1
	handler := newServer(t, cfg, &stubFetcher{cards: testdata.AllCards()})

	for i := range 3 {
		if response := get(t, handler, profileQuery, nil); response.Code != http.StatusOK {
			t.Fatalf("call %d: status = %d -- a cache hit costs no upstream fetch and must be free", i, response.Code)
		}
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

// rawQuery hits the debug outline route, which also reaches LinkedIn and so
// must be gated exactly like /profile.
const rawQuery = "/raw?url=https://www.linkedin.com/in/" + testdata.Vanity + "/"

func TestRateLimitAppliesToRawToo(t *testing.T) {
	cfg := testConfig()
	cfg.RateLimitRequests = 2
	handler := newServer(t, cfg, &stubFetcher{cards: testdata.AllCards()})

	for i := range 2 {
		if response := get(t, handler, rawQuery, nil); response.Code != http.StatusOK {
			t.Fatalf("request %d: status = %d", i, response.Code)
		}
	}
	if response := get(t, handler, rawQuery, nil); response.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429 -- /raw must not be an unmetered path to LinkedIn", response.Code)
	}
}

func TestForgedForwardedForCannotMintFreshBuckets(t *testing.T) {
	cfg := testConfig()
	cfg.RateLimitRequests = 2
	handler := newServer(t, cfg, &stubFetcher{cards: testdata.AllCards()})

	// The caller claims a different origin on every request. Our edge appends
	// the real address, so the rightmost entry stays constant and the limit
	// must still bite.
	forge := func(claimed string) map[string]string {
		return map[string]string{"X-Forwarded-For": claimed + ", 203.0.113.9"}
	}
	for i, claimed := range []string{"1.1.1.1", "2.2.2.2"} {
		if response := get(t, handler, profileQuery, forge(claimed)); response.Code != http.StatusOK {
			t.Fatalf("request %d: status = %d", i, response.Code)
		}
	}
	response := get(t, handler, profileQuery, forge("3.3.3.3"))
	if response.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429 -- rotating X-Forwarded-For must not reset the limit", response.Code)
	}
}

func TestDistinctClientsGetSeparateBuckets(t *testing.T) {
	cfg := testConfig()
	cfg.RateLimitRequests = 1
	handler := newServer(t, cfg, &stubFetcher{cards: testdata.AllCards()})

	first := get(t, handler, profileQuery, map[string]string{"X-Forwarded-For": "198.51.100.1"})
	second := get(t, handler, profileQuery, map[string]string{"X-Forwarded-For": "198.51.100.2"})
	if first.Code != http.StatusOK || second.Code != http.StatusOK {
		t.Fatalf("statuses = %d, %d; separate clients must not share a bucket", first.Code, second.Code)
	}
}

func TestUpstreamBudgetCapsFetchesAcrossAllClients(t *testing.T) {
	cfg := testConfig()
	cfg.UpstreamDaily = 2
	fetcher := &stubFetcher{cards: testdata.AllCards()}
	handler := newServer(t, cfg, fetcher)

	// Every request looks like a different caller and asks for a different
	// profile, so neither the per-IP limiter nor the cache absorbs it. Only
	// the global budget stands between this and the LinkedIn session.
	for i, vanity := range []string{"alpha", "beta"} {
		headers := map[string]string{"X-Forwarded-For": "203.0.113." + string(rune('1'+i))}
		if response := get(t, handler, "/profile?url="+vanity, headers); response.Code != http.StatusOK {
			t.Fatalf("request %d: status = %d", i, response.Code)
		}
	}

	response := get(t, handler, "/profile?url=gamma", map[string]string{"X-Forwarded-For": "203.0.113.9"})
	if response.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", response.Code)
	}
	var body model.ErrorResponse
	decode(t, response, &body)
	if body.Code != "budget_exhausted" {
		t.Errorf("code = %q, want budget_exhausted", body.Code)
	}
	if response.Header().Get("Retry-After") == "" {
		t.Error("budget 429 should carry Retry-After")
	}
	if fetcher.calls != 2 {
		t.Errorf("upstream calls = %d, want 2 -- the budget must stop the fetch, not just the response", fetcher.calls)
	}
}

func TestCachedRepeatsDoNotSpendBudget(t *testing.T) {
	cfg := testConfig()
	cfg.UpstreamDaily = 1
	fetcher := &stubFetcher{cards: testdata.AllCards()}
	handler := newServer(t, cfg, fetcher)

	for i := range 3 {
		if response := get(t, handler, profileQuery, nil); response.Code != http.StatusOK {
			t.Fatalf("request %d: status = %d -- a cache hit must not cost budget", i, response.Code)
		}
	}
	if fetcher.calls != 1 {
		t.Errorf("upstream calls = %d, want 1", fetcher.calls)
	}
}

func TestHealthReportsRemainingBudget(t *testing.T) {
	cfg := testConfig()
	cfg.UpstreamDaily = 5
	cfg.UpstreamHourly = 3
	handler := newServer(t, cfg, &stubFetcher{cards: testdata.AllCards()})

	if response := get(t, handler, profileQuery, nil); response.Code != http.StatusOK {
		t.Fatalf("status = %d", response.Code)
	}
	var health model.HealthResponse
	decode(t, get(t, handler, "/health", nil), &health)
	if health.DailyRemaining != 4 {
		t.Errorf("daily remaining = %d, want 4", health.DailyRemaining)
	}
	if health.HourlyRemaining != 2 {
		t.Errorf("hourly remaining = %d, want 2", health.HourlyRemaining)
	}
}

// When every content card fails, the shell alone still yields a name, a
// headline and a photo. Returning that as a plain 200 is indistinguishable
// from a member with no experience, education or skills -- which is how nine
// failing cards went unnoticed in production. The body stays useful and the
// status stays 200, but meta has to say what happened.
func TestDegradedFetchIsReportedNotHidden(t *testing.T) {
	cfg := testConfig()
	shellOnly := map[string]string{"shell": testdata.AllCards()["shell"]}
	fetcher := &stubFetcher{
		cards:  shellOnly,
		failed: []string{"profileCardsAboveActivity", "profileCardsExperienceOnly"},
	}
	handler := newServer(t, cfg, fetcher)

	response := get(t, handler, profileQuery, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 -- a partial profile is still useful", response.Code)
	}
	var body model.ProfileResponse
	decode(t, response, &body)

	if body.Profile.Name == "" {
		t.Error("the shell still carries identity; name should be populated")
	}
	for _, want := range []string{"profileCardsAboveActivity", "profileCardsExperienceOnly"} {
		if !slices.Contains(body.Meta.CardsFailed, want) {
			t.Errorf("meta.cards_failed = %v, missing %q", body.Meta.CardsFailed, want)
		}
	}
	if len(body.Meta.Warnings) == 0 {
		t.Fatal("meta.warnings is empty -- the caller cannot tell missing from absent")
	}
	joined := strings.Join(body.Meta.Warnings, " ")
	if !strings.Contains(joined, "could not be fetched") {
		t.Errorf("warnings do not mention the failed fetch: %v", body.Meta.Warnings)
	}
	if !strings.Contains(joined, "no content cards were returned") {
		t.Errorf("warnings do not flag the shell-only result: %v", body.Meta.Warnings)
	}
}

func TestHealthyFetchCarriesNoWarnings(t *testing.T) {
	handler := newServer(t, testConfig(), &stubFetcher{cards: testdata.AllCards()})
	var body model.ProfileResponse
	decode(t, get(t, handler, profileQuery, nil), &body)
	if len(body.Meta.Warnings) != 0 {
		t.Errorf("warnings = %v, want none on a clean fetch", body.Meta.Warnings)
	}
	if len(body.Meta.CardsFailed) != 0 {
		t.Errorf("cards_failed = %v, want none", body.Meta.CardsFailed)
	}
}
