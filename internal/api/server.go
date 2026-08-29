// Package api is the HTTP layer: routing, auth, rate limiting, caching and
// error mapping.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ayushsharma/linkedin-profile-api/internal/cache"
	"github.com/ayushsharma/linkedin-profile-api/internal/config"
	"github.com/ayushsharma/linkedin-profile-api/internal/linkedin"
	"github.com/ayushsharma/linkedin-profile-api/internal/model"
	"github.com/ayushsharma/linkedin-profile-api/internal/urlparse"
)

// Version is reported by /health.
const Version = "1.0.0"

// Fetcher retrieves the raw card responses for a profile. The server depends
// on this rather than on *linkedin.Client so tests can drive it directly.
type Fetcher interface {
	FetchProfile(ctx context.Context, vanity string, includeActivity bool) (map[string]string, error)
}

// Server holds everything the handlers need.
type Server struct {
	cfg     config.Config
	fetcher Fetcher
	cache   *cache.TTL
	limiter *cache.RateLimiter
	log     *slog.Logger
	started time.Time

	// budgets are global caps on requests that actually reach LinkedIn. The
	// per-IP limiter can be evaded by rotating source addresses; these cannot,
	// because they sit on the single code path to the upstream session.
	hourly *cache.Budget
	daily  *cache.Budget

	// gate serialises profile fetches: a burst of parallel fetches from one
	// LinkedIn session is what gets an account challenged.
	gate      sync.Mutex
	lastFetch time.Time
	sleep     func(time.Duration)
}

// New builds a Server.
func New(cfg config.Config, fetcher Fetcher, logger *slog.Logger) *Server {
	if logger == nil {
		logger = slog.Default()
	}
	return &Server{
		cfg:     cfg,
		fetcher: fetcher,
		cache:   cache.NewTTL(cfg.CacheTTL, cfg.CacheMaxEntries),
		limiter: cache.NewRateLimiter(cfg.RateLimitRequests, cfg.RateLimitWindow),
		hourly:  cache.NewBudget("hourly", cfg.UpstreamHourly, time.Hour),
		daily:   cache.NewBudget("daily", cfg.UpstreamDaily, 24*time.Hour),
		log:     logger,
		started: time.Now(),
		sleep:   time.Sleep,
	}
}

// Routes returns the configured mux.
func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", s.handleHealth)
	mux.HandleFunc("GET /profile", s.handleProfile)
	mux.HandleFunc("GET /raw", s.handleRaw)
	mux.HandleFunc("GET /openapi.json", s.handleOpenAPI)
	mux.HandleFunc("GET /", s.handleIndex)
	return s.accessLog(cors(mux))
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, model.HealthResponse{
		Status:            "ok",
		Version:           Version,
		SessionConfigured: s.cfg.SessionConfigured(),
		CacheEntries:      s.cache.Len(),
		UptimeSeconds:     int64(time.Since(s.started).Seconds()),
		HourlyRemaining:   s.hourly.Remaining(),
		DailyRemaining:    s.daily.Remaining(),
	})
}

func (s *Server) handleProfile(w http.ResponseWriter, r *http.Request) {
	if !s.authorised(w, r) {
		return
	}
	if s.throttled(w, r) {
		return
	}

	vanity, err := urlparse.ExtractVanity(r.URL.Query().Get("url"))
	if err != nil {
		writeError(w, err)
		return
	}
	includeActivity := boolParam(r, "include_activity")
	refresh := boolParam(r, "refresh")
	cacheKey := vanity + ":" + strconv.FormatBool(includeActivity)

	if !refresh {
		if cached, ok := s.cache.Get(cacheKey); ok {
			response := cached.(model.ProfileResponse)
			response.Meta.Cached = true
			s.log.Info("profile served from cache", "vanity", vanity, "include_activity", includeActivity)
			writeJSON(w, http.StatusOK, response)
			return
		}
	}
	if !s.cfg.SessionConfigured() {
		writeError(w, &linkedin.Error{
			Status: http.StatusServiceUnavailable, Code: "session_expired",
			Message: "server has no LinkedIn session configured",
		})
		return
	}

	started := time.Now()
	cards, err := s.fetchCards(r.Context(), vanity, includeActivity)
	if err != nil {
		writeError(w, err)
		return
	}
	profile, meta := linkedin.ParseProfile(vanity, cards)
	meta.DurationMS = time.Since(started).Milliseconds()
	s.log.Info("profile fetched",
		"vanity", vanity,
		"name", profile.Name,
		"include_activity", includeActivity,
		"duration_ms", meta.DurationMS,
		"bytes", meta.BytesDownloade,
		"sections", meta.SectionsFound,
		"cards_failed", meta.CardsFailed,
		"warnings", meta.Warnings,
		"hourly_remaining", s.hourly.Remaining(),
		"daily_remaining", s.daily.Remaining(),
	)

	response := model.ProfileResponse{Profile: profile, Meta: meta}
	s.cache.Set(cacheKey, response)
	writeJSON(w, http.StatusOK, response)
}

// handleRaw exposes the semantic outline before field mapping. When a field
// comes back empty this shows whether the data was missing from the page or
// lost in mapping.
func (s *Server) handleRaw(w http.ResponseWriter, r *http.Request) {
	if !s.authorised(w, r) {
		return
	}
	if s.throttled(w, r) {
		return
	}
	vanity, err := urlparse.ExtractVanity(r.URL.Query().Get("url"))
	if err != nil {
		writeError(w, err)
		return
	}
	if !s.cfg.SessionConfigured() {
		writeError(w, &linkedin.Error{
			Status: http.StatusServiceUnavailable, Code: "session_expired",
			Message: "server has no LinkedIn session configured",
		})
		return
	}
	cards, err := s.fetchCards(r.Context(), vanity, boolParam(r, "include_activity"))
	if err != nil {
		writeError(w, err)
		return
	}

	type rawBlock struct {
		Block    string   `json:"block"`
		Headings []string `json:"headings,omitempty"`
		Texts    []string `json:"texts,omitempty"`
		Images   []string `json:"images,omitempty"`
		Links    []string `json:"links,omitempty"`
	}
	out := make(map[string][]rawBlock, len(cards))
	for name, body := range cards {
		outline, err := linkedin.Outline(body)
		if err != nil {
			continue
		}
		for _, block := range outline.Walk() {
			if len(block.Texts) == 0 && len(block.Images) == 0 && len(block.Links) == 0 {
				continue
			}
			entry := rawBlock{Block: block.Label(), Headings: block.Headings, Texts: block.Texts, Links: block.Links}
			for _, image := range block.Images {
				entry.Images = append(entry.Images, image.URL(400))
			}
			out[name] = append(out[name], entry)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"vanity_name": vanity, "cards": out})
}

// fetchCards is the only path to LinkedIn. It serialises fetches, keeps a
// floor between them, and spends the global budgets -- all under one lock, so
// the check-then-spend below cannot race.
//
// A budget is spent before the fetch rather than after, so an upstream failure
// still costs a unit. That is the conservative direction: a session that is
// erroring is exactly the one that should be backing off.
func (s *Server) fetchCards(ctx context.Context, vanity string, includeActivity bool) (map[string]string, error) {
	s.gate.Lock()
	defer s.gate.Unlock()

	for _, budget := range []*cache.Budget{s.hourly, s.daily} {
		if ok, resetIn := budget.Available(); !ok {
			s.log.Warn("upstream budget exhausted",
				"budget", budget.Name(), "vanity", vanity, "resets_in_s", int(resetIn.Seconds()))
			return nil, linkedin.ErrBudgetExhausted(budget.Name(), resetIn)
		}
	}
	for _, budget := range []*cache.Budget{s.hourly, s.daily} {
		budget.Spend()
	}

	if gap := s.cfg.MinBetweenProfiles - time.Since(s.lastFetch); gap > 0 && !s.lastFetch.IsZero() {
		s.sleep(gap)
	}
	cards, err := s.fetcher.FetchProfile(ctx, vanity, includeActivity)
	s.lastFetch = time.Now()
	return cards, err
}

func (s *Server) authorised(w http.ResponseWriter, r *http.Request) bool {
	if len(s.cfg.APIKeys) == 0 {
		return true
	}
	provided := r.Header.Get("X-API-Key")
	for _, key := range s.cfg.APIKeys {
		if provided == key {
			return true
		}
	}
	s.log.Warn("unauthorized", "client", clientKey(r), "path", r.URL.Path)
	writeError(w, &linkedin.Error{
		Status: http.StatusUnauthorized, Code: "unauthorized",
		Message: "missing or invalid X-API-Key",
	})
	return false
}

// throttled applies the per-IP limit and writes the 429 if it trips. It gates
// every handler that can reach LinkedIn.
func (s *Server) throttled(w http.ResponseWriter, r *http.Request) bool {
	allowed, retryAfter := s.limiter.Check(clientKey(r))
	if allowed {
		return false
	}
	s.log.Warn("rate limited", "client", clientKey(r), "path", r.URL.Path, "retry_after_s", int(retryAfter.Seconds())+1)
	writeError(w, &linkedin.Error{
		Status: http.StatusTooManyRequests, Code: "rate_limited",
		Message:    "too many requests",
		Detail:     "per-IP limit",
		RetryAfter: retryAfter,
	})
	return true
}

// clientKey identifies the caller for rate limiting.
//
// X-Forwarded-For is caller-supplied and every proxy appends to it, so the
// leftmost entry is whatever the caller claimed and the rightmost is the one
// our own edge added. Reading the rightmost is what makes the limit survive a
// caller who rotates the header to mint fresh buckets.
func clientKey(r *http.Request) string {
	if flyIP := strings.TrimSpace(r.Header.Get("Fly-Client-IP")); flyIP != "" {
		return flyIP
	}
	if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
		parts := strings.Split(forwarded, ",")
		if last := strings.TrimSpace(parts[len(parts)-1]); last != "" {
			return last
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func boolParam(r *http.Request, name string) bool {
	value := strings.ToLower(strings.TrimSpace(r.URL.Query().Get(name)))
	return value == "1" || value == "true" || value == "yes"
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	encoder := json.NewEncoder(w)
	encoder.SetEscapeHTML(false)
	_ = encoder.Encode(body)
}

func writeError(w http.ResponseWriter, err error) {
	if domain, ok := linkedin.AsError(err); ok {
		if domain.RetryAfter > 0 {
			w.Header().Set("Retry-After", strconv.Itoa(int(domain.RetryAfter.Seconds())+1))
		}
		writeJSON(w, domain.Status, model.ErrorResponse{
			Error: domain.Message, Code: domain.Code, Detail: domain.Detail,
		})
		return
	}
	var invalid *urlparse.ErrInvalid
	if errors.As(err, &invalid) {
		writeJSON(w, http.StatusBadRequest, model.ErrorResponse{
			Error: invalid.Reason, Code: "invalid_profile_url",
		})
		return
	}
	writeJSON(w, http.StatusBadGateway, model.ErrorResponse{
		Error: err.Error(), Code: "upstream_error",
	})
}

// statusRecorder captures the status and size of a response for the access log.
type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Write(body []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	written, err := r.ResponseWriter.Write(body)
	r.bytes += written
	return written, err
}

// accessLog emits one structured line per request. This is the log to read to
// see which profile was asked for, what it returned and what it cost.
//
// It records the requested profile URL and the caller address. It never
// records headers, so the API key and the session cookies cannot reach it.
func (s *Server) accessLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		recorder := &statusRecorder{ResponseWriter: w}
		next.ServeHTTP(recorder, r)

		// The docs page and schema are static and noisy; skip them.
		if r.URL.Path == "/" || r.URL.Path == "/openapi.json" {
			return
		}
		attrs := []any{
			"method", r.Method,
			"path", r.URL.Path,
			"status", recorder.status,
			"bytes", recorder.bytes,
			"duration_ms", time.Since(started).Milliseconds(),
			"client", clientKey(r),
		}
		if requested := r.URL.Query().Get("url"); requested != "" {
			attrs = append(attrs, "profile", requested)
		}
		switch {
		case recorder.status >= 500:
			s.log.Error("request", attrs...)
		case recorder.status >= 400:
			s.log.Warn("request", attrs...)
		default:
			s.log.Info("request", attrs...)
		}
	})
}

func cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-API-Key")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
