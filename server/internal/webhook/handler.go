package webhook

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/skoppers/webfuse-activity-analyzer/server/internal/httpx"
	"github.com/skoppers/webfuse-activity-analyzer/server/internal/session"
)

// Outcomes reported in the 200 body of a verified delivery.
const (
	// OutcomeApplied means the delivery changed a session row.
	OutcomeApplied = "applied"
	// OutcomeStale means the delivery was a replay, arrived out of order, or
	// targeted an ended session; the row was left alone.
	OutcomeStale = "stale"
	// OutcomeIgnored means the category is not handled, or a participant_left
	// named a session this server has never seen.
	OutcomeIgnored = "ignored"
)

// response is the body of a 200 from POST /webhooks/webfuse.
type response struct {
	Outcome string `json:"outcome"`
}

// Handler returns the webhook routes, to be mounted at /webhooks:
//
//	POST /webfuse  apply a signed Space webhook; 200 {"outcome": "applied|stale|ignored"}
//
// Every request passes RequireSignature(key) first, so an unsigned or
// mis-signed body is a 401 and never reaches the lifecycle. A verified body
// that does not parse as an Envelope, or whose data does not parse for its
// category, is a 400 {"error": ...}. Every other verified delivery is a 200
// so Webfuse neither retries nor disables the hook: duplicates and
// out-of-order retries come back "stale" (the sequence guard lives in the
// lifecycle), unhandled categories "ignored". A lifecycle failure is a 500
// with a generic message. One info line is logged per delivery.
func Handler(lc *session.Lifecycle, key string) http.Handler {
	r := chi.NewRouter()
	r.Use(RequireSignature(key))
	r.Post("/webfuse", func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		env, err := Parse(Body(ctx))
		if err != nil {
			httpx.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		sessionID, outcome, err := apply(ctx, lc, env)
		if err != nil {
			var bad badData
			if errors.As(err, &bad) {
				httpx.Error(w, http.StatusBadRequest, bad.Error())
				return
			}
			slog.Error("webhook: apply", "category", env.Category, "session_id", sessionID,
				"sequence_id", env.SequenceID, "err", err)
			httpx.Error(w, http.StatusInternalServerError, "internal error")
			return
		}
		slog.Info("webhook", "category", env.Category, "session_id", sessionID,
			"sequence_id", env.SequenceID, "outcome", outcome)
		httpx.WriteJSON(w, http.StatusOK, response{Outcome: outcome})
	})
	return r
}

// badData marks a per-category data decode failure so the handler can
// answer 400 instead of 500.
type badData struct{ err error }

func (b badData) Error() string { return b.err.Error() }
func (b badData) Unwrap() error { return b.err }

// apply routes env to the lifecycle call for its category and returns the
// session id it named (empty for an unhandled category) and the outcome.
func apply(ctx context.Context, lc *session.Lifecycle, env Envelope) (string, string, error) {
	switch env.Category {
	case CategorySessionStarted:
		d, err := env.SessionData()
		if err != nil {
			return "", "", badData{err}
		}
		_, changed, err := lc.Started(ctx, session.StartedParams{
			ID: d.SessionID, SpaceID: d.SpaceID, StartedAt: d.StartedAt,
			ParticipantCount: d.ParticipantCount, Metadata: env.Data, Seq: env.SequenceID,
		})
		return d.SessionID, outcomeOf(changed), err

	case CategorySessionEnded:
		d, err := env.SessionData()
		if err != nil {
			return "", "", badData{err}
		}
		_, ended, err := lc.Ended(ctx, d.SessionID, env.CreatedAt, env.SequenceID)
		return d.SessionID, outcomeOf(ended), err

	case CategoryParticipantJoined:
		d, err := env.ParticipantData()
		if err != nil {
			return "", "", badData{err}
		}
		_, changed, err := lc.ParticipantsChanged(ctx, session.ParticipantsParams{
			ID: d.SessionID, SpaceID: d.SpaceID, Count: d.ParticipantCount,
			At: env.CreatedAt, Seq: env.SequenceID,
		})
		return d.SessionID, outcomeOf(changed), err

	case CategoryParticipantLeft:
		d, err := env.ParticipantData()
		if err != nil {
			return "", "", badData{err}
		}
		// The left payload carries no participant_count, so the count is
		// decremented from the stored row. It is approximate: a reconnect
		// fires left upstream without a matching joined, so it can drift low
		// until the next joined resets it. Without a row there is nothing
		// to decrement.
		s, err := lc.Get(ctx, d.SessionID)
		if errors.Is(err, session.ErrNotFound) {
			return d.SessionID, OutcomeIgnored, nil
		}
		if err != nil {
			return d.SessionID, "", err
		}
		_, changed, err := lc.ParticipantsChanged(ctx, session.ParticipantsParams{
			ID: d.SessionID, SpaceID: d.SpaceID, Count: max(s.ParticipantCount-1, 0),
			At: env.CreatedAt, Seq: env.SequenceID,
		})
		return d.SessionID, outcomeOf(changed), err
	}
	return "", OutcomeIgnored, nil
}

// outcomeOf maps a lifecycle's changed flag to its outcome.
func outcomeOf(changed bool) string {
	if changed {
		return OutcomeApplied
	}
	return OutcomeStale
}
