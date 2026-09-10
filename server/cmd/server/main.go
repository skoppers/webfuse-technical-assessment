// Command server is the Webfuse activity analyzer HTTP server.
// main.go is wiring only: load config, build deps, mount routes, run.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/skoppers/webfuse-activity-analyzer/server/internal/api"
	"github.com/skoppers/webfuse-activity-analyzer/server/internal/config"
	"github.com/skoppers/webfuse-activity-analyzer/server/internal/httpx"
	"github.com/skoppers/webfuse-activity-analyzer/server/internal/ingest"
	"github.com/skoppers/webfuse-activity-analyzer/server/internal/reaper"
	"github.com/skoppers/webfuse-activity-analyzer/server/internal/session"
	"github.com/skoppers/webfuse-activity-analyzer/server/internal/store"
	"github.com/skoppers/webfuse-activity-analyzer/server/internal/stream"
	"github.com/skoppers/webfuse-activity-analyzer/server/internal/webhook"
	"github.com/skoppers/webfuse-activity-analyzer/server/web"
)

const shutdownTimeout = 10 * time.Second

func main() {
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	slog.SetDefault(log)
	if err := run(log); err != nil {
		log.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	// ctx is cancelled on SIGINT/SIGTERM and is handed to background goroutines
	// (stream hub, reaper).
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	db, err := store.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer db.Close()
	if err := store.Migrate(ctx, db); err != nil {
		return err
	}

	hub := stream.NewHub()
	lc := session.New(store.New(db), hub)
	go reaper.Run(ctx, lc, cfg.ReaperIdle, cfg.ReaperTick)

	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           newRouter(log, cfg, lc, hub),
		ReadHeaderTimeout: 5 * time.Second,
	}
	// Open SSE streams never go idle on their own; close the hub so they end
	// and Shutdown can drain the remaining ordinary requests.
	srv.RegisterOnShutdown(hub.Close)

	errc := make(chan error, 1)
	go func() { errc <- srv.ListenAndServe() }()
	log.Info("listening", "addr", srv.Addr)

	select {
	case err := <-errc:
		return fmt.Errorf("listen: %w", err)
	case <-ctx.Done():
	}

	log.Info("shutting down", "timeout", shutdownTimeout)
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("shutdown: %w", err)
	}
	log.Info("stopped")
	return nil
}

// newRouter mounts all routes; anything not matched by a handler falls
// through to the embedded web/.
func newRouter(log *slog.Logger, cfg config.Config, lc *session.Lifecycle, hub *stream.Hub) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(httpx.RequestLogger(log))
	r.Use(middleware.Recoverer)

	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		httpx.WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
	})

	r.Mount("/api", api.Handler(lc))
	r.Mount("/ingest", ingest.Handler(lc, cfg.CORSOrigin))
	r.Mount("/stream", stream.Handler(hub))
	r.Mount("/webhooks", webhook.Handler(lc, cfg.WebhookSigningKey))
	r.Handle("/*", http.FileServerFS(web.Assets))
	return r
}
