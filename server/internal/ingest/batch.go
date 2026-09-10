// Package ingest is the HTTP adapter over session.Lifecycle.RecordEvents for
// the extension's batched event payload. It owns the /ingest wire shape
// (Batch, Event), validates a batch against the wire contract, and maps it to
// the lifecycle's NewEvent, converting epoch milliseconds to time.Time. It
// stores nothing and publishes nothing itself.
package ingest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"time"
	"unicode/utf8"

	"github.com/skoppers/webfuse-activity-analyzer/server/internal/event"
	"github.com/skoppers/webfuse-activity-analyzer/server/internal/session"
)

// Limits of the wire contract.
const (
	maxSessionIDChars = 128
	maxEvents         = 500
	maxSeq            = math.MaxInt32
	maxDataBytes      = 4 << 10
	tsWindow          = 24 * time.Hour
)

// Batch is the payload of POST /ingest as the extension sends it.
type Batch struct {
	SessionID string  `json:"session_id"`
	SpaceID   string  `json:"space_id"`
	Events    []Event `json:"events"`
}

// Event is one captured event inside a Batch. TS is epoch milliseconds.
// Data is optional and, when present, a JSON object.
type Event struct {
	Type string          `json:"type"`
	Seq  int             `json:"seq"`
	TS   int64           `json:"ts"`
	Data json.RawMessage `json:"data"`
}

// Validate checks batch against the wire contract. now anchors the timestamp
// window so callers own time. The error names the offending field, indexing
// events as events[i].
func Validate(batch Batch, now time.Time) error {
	if batch.SessionID == "" {
		return fmt.Errorf("session_id: must not be empty")
	}
	if utf8.RuneCountInString(batch.SessionID) > maxSessionIDChars {
		return fmt.Errorf("session_id: must be at most %d characters", maxSessionIDChars)
	}
	if batch.SpaceID == "" {
		return fmt.Errorf("space_id: must not be empty")
	}
	if n := len(batch.Events); n < 1 || n > maxEvents {
		return fmt.Errorf("events: must contain between 1 and %d events, got %d", maxEvents, n)
	}
	for i, e := range batch.Events {
		if err := validateEvent(e, now); err != nil {
			return fmt.Errorf("events[%d].%w", i, err)
		}
	}
	return nil
}

func validateEvent(e Event, now time.Time) error {
	if !event.Valid(event.Type(e.Type)) {
		return fmt.Errorf("type: unknown event type %q", e.Type)
	}
	if e.Seq < 1 || e.Seq > maxSeq {
		return fmt.Errorf("seq: must be between 1 and %d", maxSeq)
	}
	ts := time.UnixMilli(e.TS)
	if ts.Before(now.Add(-tsWindow)) || ts.After(now.Add(tsWindow)) {
		return fmt.Errorf("ts: must be within 1 day of the server time")
	}
	if isNullData(e.Data) {
		return nil
	}
	if len(e.Data) > maxDataBytes {
		return fmt.Errorf("data: must be at most %d bytes", maxDataBytes)
	}
	trimmed := bytes.TrimSpace(e.Data)
	if trimmed[0] != '{' || !json.Valid(trimmed) {
		return fmt.Errorf("data: must be a JSON object")
	}
	return nil
}

// isNullData reports whether data was absent or JSON null.
func isNullData(data json.RawMessage) bool {
	trimmed := bytes.TrimSpace(data)
	return len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null"))
}

// toNewEvents maps a validated batch to lifecycle events, converting epoch
// milliseconds to time.Time. Null data becomes nil so it is stored as an
// empty object.
func toNewEvents(batch Batch) []session.NewEvent {
	out := make([]session.NewEvent, len(batch.Events))
	for i, e := range batch.Events {
		var data json.RawMessage
		if !isNullData(e.Data) {
			data = e.Data
		}
		out[i] = session.NewEvent{
			Type: event.Type(e.Type),
			Seq:  e.Seq,
			TS:   time.UnixMilli(e.TS),
			Data: data,
		}
	}
	return out
}
