package ingest

import (
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/skoppers/webfuse-activity-analyzer/server/internal/httpx"
	"github.com/skoppers/webfuse-activity-analyzer/server/internal/session"
)

// response is the body of a 202 from POST /ingest.
type response struct {
	Accepted   int `json:"accepted"`
	Duplicates int `json:"duplicates"`
}

// Handler returns the ingest routes, to be mounted at /ingest:
//
//	POST /  store a Batch; 202 {"accepted": n, "duplicates": m}
//
// A body that fails to decode or validate, or a batch the lifecycle rejects,
// is a 400 {"error": ...}; any other failure is a 500 with a generic message.
// Resending a batch is safe: events already stored are counted as duplicates.
// The router answers CORS preflight and allows corsOrigin on every response.
func Handler(lc *session.Lifecycle, corsOrigin string) http.Handler {
	r := chi.NewRouter()
	r.Use(httpx.CORS(corsOrigin))
	r.Post("/", func(w http.ResponseWriter, r *http.Request) {
		var b Batch
		if err := httpx.ReadJSON(w, r, &b); err != nil {
			httpx.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		now := time.Now()
		if err := Validate(b, now); err != nil {
			httpx.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		res, err := lc.RecordEvents(r.Context(), b.SessionID, b.SpaceID, now, toNewEvents(b))
		switch {
		case errors.Is(err, session.ErrInvalidEventType), errors.Is(err, session.ErrEmptyBatch):
			httpx.Error(w, http.StatusBadRequest, err.Error())
			return
		case err != nil:
			slog.Error("ingest: record events", "session_id", b.SessionID, "err", err)
			httpx.Error(w, http.StatusInternalServerError, "internal error")
			return
		}
		httpx.WriteJSON(w, http.StatusAccepted, response{Accepted: res.Inserted, Duplicates: res.Duplicates})
	})
	return r
}
