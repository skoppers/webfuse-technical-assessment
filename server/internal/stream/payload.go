package stream

import (
	"encoding/json"
	"time"
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
// event. ClientID names the background boot that sent it; (client_id, seq)
// identifies the event within its session. TS is the client's timestamp in
// epoch milliseconds, the same unit the extension sends.
type ActivityPayload struct {
	SessionID string          `json:"session_id"`
	ClientID  string          `json:"client_id"`
	Type      string          `json:"type"`
	Seq       int             `json:"seq"`
	TS        int64           `json:"ts"`
	Data      json.RawMessage `json:"data"`
}
