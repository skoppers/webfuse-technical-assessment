package webhook

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"
)

// Webhook categories delivered for a Space.
const (
	CategorySessionStarted    = "space.session.started"
	CategorySessionEnded      = "space.session.ended"
	CategoryParticipantJoined = "space.session.participant_joined"
	CategoryParticipantLeft   = "space.session.participant_left"
)

// Envelope is the outer shape of every webhook. SequenceID orders events
// and detects duplicates; Data is decoded per category by SessionData or
// ParticipantData.
type Envelope struct {
	Category   string          `json:"category"`
	CreatedAt  time.Time       `json:"created_at"`
	SequenceID int64           `json:"sequence_id"`
	Data       json.RawMessage `json:"data"`
}

// SessionData is the data of a session started or ended webhook. Duration
// is seconds and nil until the session has ended.
type SessionData struct {
	SessionID        string    `json:"session_id"`
	SpaceID          string    `json:"space_id"`
	StartedAt        time.Time `json:"started_at"`
	ParticipantCount int       `json:"participant_count"`
	Duration         *int      `json:"duration"`
}

// ParticipantData is the data of a participant joined or left webhook.
// ParticipantCount is sent only on joined and ClientIndex only on left;
// each is zero when absent.
type ParticipantData struct {
	SessionID        string `json:"session_id"`
	SpaceID          string `json:"space_id"`
	SpaceName        string `json:"space_name"`
	ParticipantCount int    `json:"participant_count"`
	ClientIndex      int    `json:"client_index"`
}

// Parse decodes plain as an Envelope, requiring a category, a sequence_id
// of at least 1 and a parsable created_at.
func Parse(plain []byte) (Envelope, error) {
	var env Envelope
	if err := json.Unmarshal(plain, &env); err != nil {
		return Envelope{}, fmt.Errorf("invalid envelope: %w", err)
	}
	if env.Category == "" {
		return Envelope{}, errors.New("category: must not be empty")
	}
	if env.SequenceID < 1 {
		return Envelope{}, errors.New("sequence_id: must be at least 1")
	}
	if env.CreatedAt.IsZero() {
		return Envelope{}, errors.New("created_at: must be set")
	}
	return env, nil
}

// SessionData decodes e.Data as a SessionData. It does not check
// e.Category; the caller chooses the decoder by category.
func (e Envelope) SessionData() (SessionData, error) {
	var w struct {
		SessionID        string    `json:"session_id"`
		SpaceID          spaceID   `json:"space_id"`
		StartedAt        time.Time `json:"started_at"`
		ParticipantCount int       `json:"participant_count"`
		Duration         *int      `json:"duration"`
	}
	if err := json.Unmarshal(e.Data, &w); err != nil {
		return SessionData{}, fmt.Errorf("invalid session data: %w", err)
	}
	return SessionData{
		SessionID:        w.SessionID,
		SpaceID:          string(w.SpaceID),
		StartedAt:        w.StartedAt,
		ParticipantCount: w.ParticipantCount,
		Duration:         w.Duration,
	}, nil
}

// ParticipantData decodes e.Data as a ParticipantData. It does not check
// e.Category; the caller chooses the decoder by category.
func (e Envelope) ParticipantData() (ParticipantData, error) {
	var w struct {
		SessionID        string  `json:"session_id"`
		SpaceID          spaceID `json:"space_id"`
		SpaceName        string  `json:"space_name"`
		ParticipantCount int     `json:"participant_count"`
		ClientIndex      int     `json:"client_index"`
	}
	if err := json.Unmarshal(e.Data, &w); err != nil {
		return ParticipantData{}, fmt.Errorf("invalid participant data: %w", err)
	}
	return ParticipantData{
		SessionID:        w.SessionID,
		SpaceID:          string(w.SpaceID),
		SpaceName:        w.SpaceName,
		ParticipantCount: w.ParticipantCount,
		ClientIndex:      w.ClientIndex,
	}, nil
}

// spaceID accepts a JSON number (as Webfuse sends it) or a string and
// yields the string form, e.g. 23 -> "23".
type spaceID string

func (s *spaceID) UnmarshalJSON(b []byte) error {
	var v any
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	if err := dec.Decode(&v); err != nil {
		return err
	}
	switch v := v.(type) {
	case json.Number:
		*s = spaceID(v.String())
	case string:
		*s = spaceID(v)
	default:
		return fmt.Errorf("space_id: must be a number or string, got %s", strconv.Quote(string(b)))
	}
	return nil
}
