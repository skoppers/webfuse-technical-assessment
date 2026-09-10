// Package session owns the server-side session lifecycle: it is the one
// module that turns webhooks, ingested session events and reaper ticks into
// persisted changes and the stream messages that announce them. The Store
// and the stream Hub are internal to it; every write publishes only when a
// row actually changed, so callers never publish themselves.
package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/skoppers/webfuse-activity-analyzer/server/internal/event"
	"github.com/skoppers/webfuse-activity-analyzer/server/internal/jsonx"
	"github.com/skoppers/webfuse-activity-analyzer/server/internal/store"
	"github.com/skoppers/webfuse-activity-analyzer/server/internal/stream"
)

// Session and Event are the persisted rows as callers of this package see
// them. They alias the store types so callers never import store.
type (
	Session        = store.Session
	SessionSummary = store.SessionSummary
	Event          = store.Event
)

// Session status values.
const (
	StatusLive  = store.StatusLive
	StatusEnded = store.StatusEnded
)

var (
	// ErrNotFound is returned by Get when no session has the given id.
	ErrNotFound = store.ErrNotFound
	// ErrInvalidEventType rejects a RecordEvents batch containing a type
	// outside the event contract. Nothing from the batch is stored.
	ErrInvalidEventType = errors.New("session: invalid event type")
	// ErrEmptyBatch rejects a RecordEvents call with no events.
	ErrEmptyBatch = errors.New("session: empty batch")
)

// Lifecycle applies lifecycle changes and activity to sessions and fans the
// results out to stream subscribers. Construct with New.
type Lifecycle struct {
	st  *store.Store
	hub *stream.Hub
}

// New returns a Lifecycle over the given store and hub.
func New(st *store.Store, hub *stream.Hub) *Lifecycle {
	return &Lifecycle{st: st, hub: hub}
}

// StartedParams carries a session.started webhook. Seq is the webhook sequence id.
type StartedParams struct {
	ID               string
	SpaceID          string
	StartedAt        time.Time
	ParticipantCount int
	Metadata         json.RawMessage
	Seq              int64
}

// ParticipantsParams carries a participant_joined or participant_left webhook.
// At is used as the start time when the session is not yet known.
type ParticipantsParams struct {
	ID      string
	SpaceID string
	Count   int
	At      time.Time
	Seq     int64
}

// NewEvent is one session event from an ingest batch.
type NewEvent struct {
	Type event.Type
	Seq  int
	TS   time.Time
	Data json.RawMessage
}

// RecordResult reports what RecordEvents did with a batch.
type RecordResult struct {
	Session    Session
	Inserted   int
	Duplicates int
}

// Started applies a session.started webhook: the session is created if
// absent, then enriched with the webhook's fields unless the session has
// ended or the seq is stale. changed is false when neither happened (stale
// or replayed webhook); nothing is published then. The returned session is
// the current row either way.
func (l *Lifecycle) Started(ctx context.Context, p StartedParams) (Session, bool, error) {
	s, inserted, err := l.st.FindOrCreateSession(ctx, p.ID, p.SpaceID, p.StartedAt, store.SourceWebhook)
	if err != nil {
		return Session{}, false, err
	}
	enriched, updated, err := l.st.EnrichSessionFromWebhook(ctx, store.EnrichParams{
		ID: p.ID, SpaceID: p.SpaceID, StartedAt: p.StartedAt,
		ParticipantCount: p.ParticipantCount, Metadata: p.Metadata, Seq: p.Seq,
	})
	if err != nil {
		return Session{}, false, err
	}
	if updated {
		s = enriched
	}
	changed := inserted || updated
	if changed {
		l.hub.Publish(sessionMessage(s))
	}
	return s, changed, nil
}

// Ended applies a session.ended webhook. ended is false when the session is
// unknown, already ended, or seq is stale; nothing is published then.
func (l *Lifecycle) Ended(ctx context.Context, id string, at time.Time, seq int64) (Session, bool, error) {
	s, ended, err := l.st.EndSession(ctx, id, at, seq)
	if err != nil {
		return Session{}, false, err
	}
	if ended {
		l.hub.Publish(sessionMessage(s))
	}
	return s, ended, nil
}

// ParticipantsChanged applies a participant count webhook. An unknown session
// is created first so a missed session.started does not lose the count.
// updated is false when the session has ended or seq is stale.
func (l *Lifecycle) ParticipantsChanged(ctx context.Context, p ParticipantsParams) (Session, bool, error) {
	s, inserted, err := l.st.FindOrCreateSession(ctx, p.ID, p.SpaceID, p.At, store.SourceWebhook)
	if err != nil {
		return Session{}, false, err
	}
	updated, changed, err := l.st.SetParticipantCount(ctx, p.ID, p.Count, p.Seq)
	if err != nil {
		return Session{}, false, err
	}
	if changed {
		s = updated
	}
	if inserted || changed {
		l.hub.Publish(sessionMessage(s))
	}
	return s, changed, nil
}

// RecordEvents stores an ingest batch for a session. Every type must be in
// the event contract or the whole batch is rejected with ErrInvalidEventType.
// An unknown session is created as a live stub (source event) started at now,
// the server's receive time, and announced; an ended session is never revived
// but its late events are still stored.
// Events already stored for (session, seq) are counted as duplicates. Each
// newly stored event is published as an activity message.
func (l *Lifecycle) RecordEvents(ctx context.Context, sessionID, spaceID string, now time.Time, events []NewEvent) (RecordResult, error) {
	if len(events) == 0 {
		return RecordResult{}, ErrEmptyBatch
	}
	for _, e := range events {
		if !event.Valid(e.Type) {
			return RecordResult{}, fmt.Errorf("%w: %q", ErrInvalidEventType, e.Type)
		}
	}

	s, inserted, err := l.st.FindOrCreateSession(ctx, sessionID, spaceID, now, store.SourceEvent)
	if err != nil {
		return RecordResult{}, err
	}
	if inserted {
		l.hub.Publish(sessionMessage(s))
	}

	res := RecordResult{Session: s}
	for _, e := range events {
		id, ok, err := l.st.InsertEvent(ctx, sessionID, e.Seq, string(e.Type), e.TS, e.Data)
		if err != nil {
			return res, err
		}
		if !ok {
			res.Duplicates++
			continue
		}
		res.Inserted++
		l.hub.Publish(activityMessage(Event{
			ID: id, SessionID: sessionID, Seq: e.Seq, Type: string(e.Type), TS: e.TS, Data: e.Data,
		}))
	}
	return res, nil
}

// ReapIdle ends every live session whose last activity (or start, when it
// has none) is more than idle before now, publishing each. It returns the
// sessions it ended. now is taken as a parameter so the caller owns time.
func (l *Lifecycle) ReapIdle(ctx context.Context, now time.Time, idle time.Duration) ([]Session, error) {
	candidates, err := l.st.ListIdleLiveSessions(ctx, now.Add(-idle))
	if err != nil {
		return nil, err
	}
	ended := make([]Session, 0, len(candidates))
	for _, c := range candidates {
		s, ok, err := l.st.EndSession(ctx, c.ID, now, 0)
		if err != nil {
			return ended, err
		}
		if !ok { // ended by a webhook since we listed it
			continue
		}
		l.hub.Publish(sessionMessage(s))
		ended = append(ended, s)
	}
	return ended, nil
}

// Get returns the session with the given id, or ErrNotFound.
func (l *Lifecycle) Get(ctx context.Context, id string) (Session, error) {
	return l.st.GetSession(ctx, id)
}

// List returns up to limit sessions, live first, newest first within each,
// each with its key event count and latest key event.
func (l *Lifecycle) List(ctx context.Context, limit int) ([]SessionSummary, error) {
	return l.st.ListSessions(ctx, limit, event.KeyTypes())
}

// Events returns every stored event of a session ordered by ts, then seq.
func (l *Lifecycle) Events(ctx context.Context, id string) ([]Event, error) {
	return l.st.ListEventsBySession(ctx, id)
}

// sessionMessage builds the "session" stream message for a session row.
func sessionMessage(s Session) stream.Message {
	return stream.Message{
		Event:     stream.EventSession,
		SessionID: s.ID,
		Data: stream.SessionPayload{
			SessionID:        s.ID,
			SpaceID:          s.SpaceID,
			Status:           s.Status,
			StartedAt:        s.StartedAt,
			EndedAt:          s.EndedAt,
			ParticipantCount: s.ParticipantCount,
		},
	}
}

// activityMessage builds the "activity" stream message for an event row.
// Key events are marked so the overview stream carries them too. Empty Data
// is sent as an empty object.
func activityMessage(e Event) stream.Message {
	return stream.Message{
		Event:     stream.EventActivity,
		SessionID: e.SessionID,
		Key:       event.IsKey(event.Type(e.Type)),
		Data: stream.ActivityPayload{
			SessionID: e.SessionID,
			Type:      e.Type,
			Seq:       e.Seq,
			TS:        e.TS.UnixMilli(),
			Data:      jsonx.OrEmptyObject(e.Data),
		},
	}
}
