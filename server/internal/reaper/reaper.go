// Package reaper owns the schedule on which idle live sessions are closed:
// one sweep at startup, then one per tick until the context is done. It
// defers the idle rule and the ending itself to Lifecycle.ReapIdle and only
// logs what that returned; it stores nothing and publishes nothing itself.
package reaper

import (
	"context"
	"log/slog"
	"time"

	"github.com/skoppers/webfuse-activity-analyzer/server/internal/session"
)

// Run sweeps once immediately, then on every tick until ctx is done. The
// immediate sweep closes sessions a previous process left live. A failed
// sweep is logged and the loop carries on; Run only returns when ctx is
// done, and never sweeps after that.
func Run(ctx context.Context, lc *session.Lifecycle, idle, tick time.Duration) {
	ticker := time.NewTicker(tick)
	defer ticker.Stop()
	for {
		if ctx.Err() != nil {
			return
		}
		sweep(ctx, lc, idle)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// sweep runs one ReapIdle pass and logs its outcome.
func sweep(ctx context.Context, lc *session.Lifecycle, idle time.Duration) {
	ended, err := lc.ReapIdle(ctx, time.Now(), idle)
	for _, s := range ended {
		slog.Info("session reaped", "session_id", s.ID, "reason", "idle")
	}
	if err != nil {
		slog.Error("reaper sweep", "err", err)
		return
	}
	if len(ended) == 0 {
		slog.Debug("reaper sweep", "ended", 0)
	}
}
