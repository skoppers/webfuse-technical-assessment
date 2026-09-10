package api

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/skoppers/webfuse-activity-analyzer/server/internal/httpx"
	"github.com/skoppers/webfuse-activity-analyzer/server/internal/session"
)

// Session list limits: the default when ?limit is absent and the cap a
// larger request is clamped to.
const (
	defaultLimit = 100
	maxLimit     = 500
)

// listResponse is the body of GET /sessions.
type listResponse struct {
	Sessions []SessionSummary `json:"sessions"`
}

// eventsResponse is the body of GET /sessions/{id}/events.
type eventsResponse struct {
	SessionID string  `json:"session_id"`
	Events    []Event `json:"events"`
}

// Handler returns the read routes, to be mounted at /api:
//
//	GET /sessions?limit=N      200 {"sessions": [SessionSummary...]}, live first, newest first
//	GET /sessions/{id}         200 Session
//	GET /sessions/{id}/events  200 {"session_id": id, "events": [Event...]}, by ts then seq
//
// limit defaults to 100 and is clamped to 500; anything that is not a
// positive integer is a 400. An unknown session is a 404 {"error": "not
// found"}; any other failure is a 500 with a generic message. Every response
// is Cache-Control: no-store.
func Handler(lc *session.Lifecycle) http.Handler {
	h := &handler{lc: lc}
	r := chi.NewRouter()
	r.Use(httpx.NoStore)
	r.Get("/sessions", h.list)
	r.Get("/sessions/{id}", h.get)
	r.Get("/sessions/{id}/events", h.events)
	return r
}

type handler struct {
	lc *session.Lifecycle
}

func (h *handler) list(w http.ResponseWriter, r *http.Request) {
	limit, err := parseLimit(r.URL.Query().Get("limit"))
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	rows, err := h.lc.List(r.Context(), limit)
	if err != nil {
		slog.Error("api: list sessions", "err", err)
		httpx.Error(w, http.StatusInternalServerError, "internal error")
		return
	}
	out := make([]SessionSummary, 0, len(rows))
	for _, s := range rows {
		out = append(out, toSummary(s))
	}
	httpx.WriteJSON(w, http.StatusOK, listResponse{Sessions: out})
}

func (h *handler) get(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	s, ok := h.lookup(w, r, id)
	if !ok {
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toSession(s))
}

func (h *handler) events(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if _, ok := h.lookup(w, r, id); !ok {
		return
	}
	rows, err := h.lc.Events(r.Context(), id)
	if err != nil {
		slog.Error("api: list events", "session_id", id, "err", err)
		httpx.Error(w, http.StatusInternalServerError, "internal error")
		return
	}
	out := make([]Event, 0, len(rows))
	for _, e := range rows {
		out = append(out, toEvent(e))
	}
	httpx.WriteJSON(w, http.StatusOK, eventsResponse{SessionID: id, Events: out})
}

// lookup fetches session id, writing a 404 or 500 and returning false when
// it cannot.
func (h *handler) lookup(w http.ResponseWriter, r *http.Request, id string) (session.Session, bool) {
	s, err := h.lc.Get(r.Context(), id)
	switch {
	case errors.Is(err, session.ErrNotFound):
		httpx.Error(w, http.StatusNotFound, "not found")
		return session.Session{}, false
	case err != nil:
		slog.Error("api: get session", "session_id", id, "err", err)
		httpx.Error(w, http.StatusInternalServerError, "internal error")
		return session.Session{}, false
	}
	return s, true
}

// parseLimit turns the ?limit query value into a row limit: the default when
// absent, clamped to maxLimit, and an error for anything that is not a
// positive integer.
func parseLimit(raw string) (int, error) {
	if raw == "" {
		return defaultLimit, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 {
		return 0, errors.New("limit: must be a positive integer")
	}
	return min(n, maxLimit), nil
}
