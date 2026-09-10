package stream

import (
	"encoding/json"
	"time"

	"github.com/skoppers/webfuse-activity-analyzer/server/internal/store"
)

// SessionPayload is the data of a "session" message: the current state of a
// session after a lifecycle change. Timestamps are RFC 3339; ended_at is
// null while the session is live.
type SessionPayload struct {
	SessionID        string     `json:"session_id"`
	SpaceID          string     `json:"space_id"`
	Status           string     `json:"status"`
	StartedAt        time.Time  `json:"started_at"`
	EndedAt          *time.Time `json:"ended_at"`
	ParticipantCount int        `json:"participant_count"`
}

// ActivityPayload is the data of an "activity" message: one captured session
// event. TS is the client's timestamp in epoch milliseconds, the same unit the
// extension sends.
type ActivityPayload struct {
	SessionID string          `json:"session_id"`
	Type      string          `json:"type"`
	Seq       int             `json:"seq"`
	TS        int64           `json:"ts"`
	Data      json.RawMessage `json:"data"`
}

// SessionMessage builds the "session" message for a session row.
func SessionMessage(s store.Session) Message {
	return Message{
		Event:     EventSession,
		SessionID: s.ID,
		Data: SessionPayload{
			SessionID:        s.ID,
			SpaceID:          s.SpaceID,
			Status:           s.Status,
			StartedAt:        s.StartedAt,
			EndedAt:          s.EndedAt,
			ParticipantCount: s.ParticipantCount,
		},
	}
}

// ActivityMessage builds the "activity" message for an event row. key marks
// the event as one the overview stream should carry as well; the caller
// decides because the set of key event types is defined where events are
// validated. Empty Data is sent as an empty object.
func ActivityMessage(e store.Event, key bool) Message {
	data := e.Data
	if len(data) == 0 {
		data = json.RawMessage(`{}`)
	}
	return Message{
		Event:     EventActivity,
		SessionID: e.SessionID,
		Key:       key,
		Data: ActivityPayload{
			SessionID: e.SessionID,
			Type:      e.Type,
			Seq:       e.Seq,
			TS:        e.TS.UnixMilli(),
			Data:      data,
		},
	}
}
