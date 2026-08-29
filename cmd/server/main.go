// Command server runs the LinkedIn Profile API.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ayush3160/linkedin-profile-api-go/internal/api"
	"github.com/ayush3160/linkedin-profile-api-go/internal/config"
	"github.com/ayush3160/linkedin-profile-api-go/internal/linkedin"
)

func main() {
	cfg := config.Load(".env")
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: parseLevel(cfg.LogLevel)}))
	slog.SetDefault(logger)

	var fetcher api.Fetcher = unconfigured{}
	if cfg.SessionConfigured() {
		client, err := linkedin.New(linkedin.Options{
			LiAt:        cfg.LiAt,
			JSessionID:  cfg.JSessionID,
			Timeout:     cfg.UpstreamTimeout,
			Concurrency: cfg.UpstreamConcurrency,
			Logger:      logger,
		})
		if err != nil {
			logger.Error("could not build LinkedIn client", "err", err)
			os.Exit(1)
		}
		fetcher = client
	} else {
		logger.Warn("LI_AT / LI_JSESSIONID are not set -- /profile will return 503")
	}

	server := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           api.New(cfg, fetcher, logger).Routes(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		// Generous: the activity card alone can be several MB.
		WriteTimeout: 3 * time.Minute,
		IdleTimeout:  90 * time.Second,
	}

	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, os.Interrupt, syscall.SIGTERM)

	go func() {
		logger.Info("listening", "addr", server.Addr, "version", api.Version)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("server failed", "err", err)
			os.Exit(1)
		}
	}()

	<-shutdown
	logger.Info("shutting down")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		logger.Error("graceful shutdown failed", "err", err)
	}
}

// unconfigured stands in when no session cookies are set, so the service still
// starts and /health can report why /profile is unavailable.
type unconfigured struct{}

func (unconfigured) FetchProfile(context.Context, string, bool) (map[string]string, error) {
	return nil, &linkedin.Error{
		Status: http.StatusServiceUnavailable, Code: "session_expired",
		Message: "server has no LinkedIn session configured",
	}
}

func parseLevel(name string) slog.Level {
	var level slog.Level
	if err := level.UnmarshalText([]byte(name)); err != nil {
		return slog.LevelInfo
	}
	return level
}
