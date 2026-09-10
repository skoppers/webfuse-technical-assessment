// Package api serves the dashboard's read endpoints: the session list, one
// session's detail and its full event log. It is a thin adapter over the
// session Lifecycle; the response types here are the only JSON shapes it
// encodes, and each is built from a session row by its one mapping function.
package api

import (
	"encoding/json"
	"time"

	"github.com/skoppers/webfuse-activity-analyzer/server/internal/jsonx"
	"github.com/skoppers/webfuse-activity-analyzer/server/internal/session"
)

// Session is one session as the read API returns it. It carries the same
// fields and names as the stream's session payload plus source and metadata.
// Timestamps are RFC 3339; ended_at is null while the session is live.
type Session struct {
	SessionID        string          `json:"session_id"`
	SpaceID          string          `json:"space_id"`
	Status           string          `json:"status"`
	StartedAt        time.Time       `json:"started_at"`
	EndedAt          *time.Time      `json:"ended_at"`
	ParticipantCount int             `json:"participant_count"`
	Source           string          `json:"source"`
	Metadata         json.RawMessage `json:"metadata"`
}

// Event is one stored session event. TS is the client's timestamp in epoch
// milliseconds, the unit ingest accepts, so replay works in the same unit;
// ReceivedAt is when the server stored it, RFC 3339.
type Event struct {
	Seq        int             `json:"seq"`
	Type       string          `json:"type"`
	TS         int64           `json:"ts"`
	ReceivedAt time.Time       `json:"received_at"`
	Data       json.RawMessage `json:"data"`
}

// toSession maps a session row to its response shape.
func toSession(s session.Session) Session {
	return Session{
		SessionID:        s.ID,
		SpaceID:          s.SpaceID,
		Status:           s.Status,
		StartedAt:        s.StartedAt,
		EndedAt:          s.EndedAt,
		ParticipantCount: s.ParticipantCount,
		Source:           s.Source,
		Metadata:         jsonx.OrEmptyObject(s.Metadata),
	}
}

// toEvent maps an event row to its response shape.
func toEvent(e session.Event) Event {
	return Event{
		Seq:        e.Seq,
		Type:       e.Type,
		TS:         e.TS.UnixMilli(),
		ReceivedAt: e.ReceivedAt,
		Data:       jsonx.OrEmptyObject(e.Data),
	}
}
