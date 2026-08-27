package linkedin

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ayushsharma/linkedin-profile-api/internal/testdata"
)

func newTestClient(t *testing.T, handler http.Handler) (*Client, *httptest.Server) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client, err := New(Options{
		LiAt: "test-li-at", JSessionID: "ajax:1234567890",
		BaseURL: server.URL, Concurrency: 4,
	})
	if err != nil {
		t.Fatal(err)
	}
	return client, server
}

// upstream serves the shell for the navigation POST and a fixture per card.
func upstream(cards map[string]string, override func(short string) (int, string, bool)) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/flagship-web/in/") {
			if status, body, ok := override("navigate"); ok {
				w.WriteHeader(status)
				_, _ = io.WriteString(w, body)
				return
			}
			_, _ = io.WriteString(w, cards["shell"])
			return
		}
		componentID := r.URL.Query().Get("componentId")
		short := componentID[strings.LastIndex(componentID, ".")+1:]
		if status, body, ok := override(short); ok {
			w.WriteHeader(status)
			_, _ = io.WriteString(w, body)
			return
		}
		if body, ok := cards[short]; ok {
			_, _ = io.WriteString(w, body)
			return
		}
		_, _ = io.WriteString(w, "0:null")
	})
}

func noOverride(string) (int, string, bool) { return 0, "", false }

func TestFetchProfileDiscoversAndReplays(t *testing.T) {
	cards := testdata.AllCards()
	client, _ := newTestClient(t, upstream(cards, noOverride))

	got, err := client.FetchProfile(context.Background(), testdata.Vanity, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"shell", "profileCardsAboveActivity", "profileCardsExperienceOnly"} {
		if _, ok := got[want]; !ok {
			t.Errorf("missing card %q (have %v)", want, keys(got))
		}
	}
}

func TestRequestCarriesAuthAndCSRFHeaders(t *testing.T) {
	var captured *http.Request
	cards := testdata.AllCards()
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if captured == nil {
			captured = r.Clone(r.Context())
		}
		_, _ = io.WriteString(w, cards["shell"])
	})
	client, _ := newTestClient(t, handler)
	if _, err := client.Navigate(context.Background(), testdata.Vanity); err != nil {
		t.Fatal(err)
	}

	// LinkedIn requires the csrf header to equal the JSESSIONID cookie.
	if got := captured.Header.Get("csrf-token"); got != "ajax:1234567890" {
		t.Errorf("csrf-token = %q", got)
	}
	cookie := captured.Header.Get("cookie")
	for _, want := range []string{"li_at=test-li-at", `JSESSIONID="ajax:1234567890"`} {
		if !strings.Contains(cookie, want) {
			t.Errorf("cookie %q missing %q", cookie, want)
		}
	}
	if got := captured.Header.Get("x-li-rsc-stream"); got != "true" {
		t.Errorf("x-li-rsc-stream = %q", got)
	}
	if captured.Method != http.MethodPost {
		t.Errorf("method = %s, want POST", captured.Method)
	}
}

func TestRejectedSessionIsSessionExpired(t *testing.T) {
	client, _ := newTestClient(t, upstream(nil, func(what string) (int, string, bool) {
		return http.StatusForbidden, "", what == "navigate"
	}))
	_, err := client.FetchProfile(context.Background(), testdata.Vanity, false)
	assertCode(t, err, "session_expired", 503)
}

func TestAuthWallRedirectIsSessionExpired(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Location", "/authwall")
		w.WriteHeader(http.StatusFound)
	})
	client, _ := newTestClient(t, handler)
	_, err := client.FetchProfile(context.Background(), testdata.Vanity, false)
	assertCode(t, err, "session_expired", 503)
}

func TestUpstream429Surfaces(t *testing.T) {
	client, _ := newTestClient(t, upstream(nil, func(what string) (int, string, bool) {
		return http.StatusTooManyRequests, "", what == "navigate"
	}))
	_, err := client.FetchProfile(context.Background(), testdata.Vanity, false)
	assertCode(t, err, "upstream_rate_limited", 503)
}

func TestMissingProfileIs404(t *testing.T) {
	client, _ := newTestClient(t, upstream(nil, func(what string) (int, string, bool) {
		return http.StatusNotFound, "", what == "navigate"
	}))
	_, err := client.FetchProfile(context.Background(), testdata.Vanity, false)
	assertCode(t, err, "profile_not_found", 404)
}

// One dud card must not sink the whole profile.
func TestOneFailedCardIsSkipped(t *testing.T) {
	cards := testdata.AllCards()
	client, _ := newTestClient(t, upstream(cards, func(what string) (int, string, bool) {
		return http.StatusInternalServerError, "boom", what == "profileCardsExperienceOnly"
	}))
	got, err := client.FetchProfile(context.Background(), testdata.Vanity, false)
	if err != nil {
		t.Fatalf("a single card failure should not be fatal: %v", err)
	}
	if _, ok := got["profileCardsExperienceOnly"]; ok {
		t.Error("failed card should be absent, not empty")
	}
	if _, ok := got["shell"]; !ok {
		t.Error("shell should still be present")
	}
}

// A session that dies mid-fetch is fatal, since every remaining card will fail
// the same way.
func TestSessionExpiryOnACardIsFatal(t *testing.T) {
	cards := testdata.AllCards()
	client, _ := newTestClient(t, upstream(cards, func(what string) (int, string, bool) {
		return http.StatusUnauthorized, "", what == "profileCardsExperienceOnly"
	}))
	_, err := client.FetchProfile(context.Background(), testdata.Vanity, false)
	assertCode(t, err, "session_expired", 503)
}

func TestNewRequiresBothCookies(t *testing.T) {
	if _, err := New(Options{LiAt: "x"}); err == nil {
		t.Error("missing JSESSIONID should error")
	}
	if _, err := New(Options{JSessionID: "ajax:1"}); err == nil {
		t.Error("missing li_at should error")
	}
	// Quoted JSESSIONID values are accepted and normalised.
	client, err := New(Options{LiAt: "x", JSessionID: `"ajax:1"`})
	if err != nil {
		t.Fatal(err)
	}
	if client.jsessionID != "ajax:1" {
		t.Errorf("jsessionID = %q", client.jsessionID)
	}
}

func assertCode(t *testing.T, err error, code string, status int) {
	t.Helper()
	domain, ok := AsError(err)
	if !ok {
		t.Fatalf("err = %v, want a *linkedin.Error", err)
	}
	if domain.Code != code || domain.Status != status {
		t.Errorf("err = %s/%d, want %s/%d", domain.Code, domain.Status, code, status)
	}
}

func keys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
