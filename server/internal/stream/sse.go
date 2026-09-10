package stream

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/skoppers/webfuse-activity-analyzer/server/internal/httpx"
)

// pingInterval is how often an idle stream sends a comment line so proxies
// do not close it.
const pingInterval = 15 * time.Second

// Handler returns the SSE routes, to be mounted at /stream:
//
//	GET /            overview: session messages and key activity
//	GET /{sessionID} everything for one session
//
// Each message is written as an id/event/data frame and flushed. A stream
// ends when the client disconnects, the request context is cancelled, or the
// hub drops the subscriber for being too slow. Last-Event-ID is logged but
// nothing is replayed; clients re-read current state from the read API after
// reconnecting.
func Handler(hub *Hub) http.Handler {
	return newHandler(hub, pingInterval)
}

type handler struct {
	hub  *Hub
	ping time.Duration
}

// newHandler is Handler with a configurable keep-alive interval.
func newHandler(hub *Hub, ping time.Duration) http.Handler {
	h := &handler{hub: hub, ping: ping}
	r := chi.NewRouter()
	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		h.serve(w, r, Filter{})
	})
	r.Get("/{sessionID}", func(w http.ResponseWriter, r *http.Request) {
		h.serve(w, r, Filter{SessionID: chi.URLParam(r, "sessionID")})
	})
	return r
}

func (h *handler) serve(w http.ResponseWriter, r *http.Request, filter Filter) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		httpx.Error(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}
	if last := r.Header.Get("Last-Event-ID"); last != "" {
		slog.Debug("stream: client reconnected", "last_event_id", last, "session_id", filter.SessionID)
	}

	// Subscribe before sending headers so nothing published between the
	// two is missed.
	ch, cancel := h.hub.Subscribe(filter)
	defer cancel()

	hdr := w.Header()
	hdr.Set("Content-Type", "text/event-stream")
	hdr.Set("Cache-Control", "no-cache")
	hdr.Set("Connection", "keep-alive")
	hdr.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	ticker := time.NewTicker(h.ping)
	defer ticker.Stop()
	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-ch:
			if !ok {
				return
			}
			if err := writeFrame(w, msg); err != nil {
				return
			}
			flusher.Flush()
		case <-ticker.C:
			if _, err := io.WriteString(w, ": ping\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

// writeFrame writes one SSE frame. The data line is a single line of JSON.
func writeFrame(w io.Writer, msg Message) error {
	data, err := json.Marshal(msg.Data)
	if err != nil {
		slog.Error("stream: encode message", "id", msg.ID, "event", msg.Event, "err", err)
		return nil
	}
	_, err = fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", msg.ID, msg.Event, data)
	return err
}
