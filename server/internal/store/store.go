package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/skoppers/webfuse-activity-analyzer/server/internal/store/gen"
)

// Session status and source values as stored in the sessions table.
const (
	StatusLive  = "live"
	StatusEnded = "ended"

	SourceWebhook = "webhook"
	SourceEvent   = "event"
)

// ErrNotFound is returned when a lookup by id matches no row.
var ErrNotFound = errors.New("store: not found")

// emptyJSONObject is stored when a caller passes no metadata or event data,
// keeping the jsonb columns non-null.
var emptyJSONObject = json.RawMessage(`{}`)

// Session is a row of the sessions table as seen by the rest of the server.
type Session struct {
	ID               string
	SpaceID          string
	Status           string
	StartedAt        time.Time
	EndedAt          *time.Time
	Source           string
	ParticipantCount int
	Metadata         json.RawMessage
	LastWebhookSeq   int64
}

// Event is a row of the events table. TS is the event's own timestamp as
// reported by the client; ReceivedAt is when the server stored it.
type Event struct {
	ID         int64
	SessionID  string
	Seq        int
	Type       string
	TS         time.Time
	ReceivedAt time.Time
	Data       json.RawMessage
}

// EnrichParams carries the fields of a session.started webhook.
type EnrichParams struct {
	ID               string
	SpaceID          string
	StartedAt        time.Time
	ParticipantCount int
	Metadata         json.RawMessage
	// Seq is the webhook sequence id; the update is skipped unless it is
	// greater than the session's last applied seq.
	Seq int64
}

// Store runs the server's queries against Postgres. It is the only type other
// packages use to read or write sessions and events.
type Store struct {
	db *sql.DB
	q  *gen.Queries
}

// New wraps an open database handle.
func New(db *sql.DB) *Store {
	return &Store{db: db, q: gen.New(db)}
}

// FindOrCreateSession returns the session with the given id, inserting a live
// one with the given space, start time and source when none exists. inserted
// reports whether this call created the row.
func (s *Store) FindOrCreateSession(ctx context.Context, id, spaceID string, startedAt time.Time, source string) (Session, bool, error) {
	row, err := s.q.InsertSessionIfAbsent(ctx, gen.InsertSessionIfAbsentParams{
		SessionID: id,
		SpaceID:   spaceID,
		StartedAt: startedAt,
		Source:    source,
	})
	if err == nil {
		return sessionFromRow(row), true, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return Session{}, false, fmt.Errorf("insert session %q: %w", id, err)
	}
	existing, err := s.GetSession(ctx, id)
	if err != nil {
		return Session{}, false, err
	}
	return existing, false, nil
}

// EnrichSessionFromWebhook applies a session.started webhook to a live
// session. updated is false when the session is missing, already ended, or
// has already applied a webhook with a seq >= p.Seq; in that case the
// returned Session is zero.
func (s *Store) EnrichSessionFromWebhook(ctx context.Context, p EnrichParams) (Session, bool, error) {
	row, err := s.q.EnrichSessionFromWebhook(ctx, gen.EnrichSessionFromWebhookParams{
		SpaceID:          p.SpaceID,
		StartedAt:        p.StartedAt,
		ParticipantCount: int32(p.ParticipantCount),
		Metadata:         orEmptyObject(p.Metadata),
		Seq:              p.Seq,
		SessionID:        p.ID,
	})
	return updatedSession(row, err, "enrich session", p.ID)
}

// EndSession marks a live session ended at the given time. seq is the
// webhook sequence id; pass 0 to end the session regardless of sequence (the
// reaper). ended is false when the session is missing, already ended, or seq
// is stale; in that case the returned Session is zero.
func (s *Store) EndSession(ctx context.Context, id string, at time.Time, seq int64) (Session, bool, error) {
	row, err := s.q.EndSession(ctx, gen.EndSessionParams{
		EndedAt:   sql.NullTime{Time: at, Valid: true},
		Seq:       seq,
		SessionID: id,
	})
	return updatedSession(row, err, "end session", id)
}

// SetParticipantCount stores the participant count from a webhook with
// sequence id seq. updated is false when the session is missing, already
// ended, or seq is stale; in that case the returned Session is zero.
func (s *Store) SetParticipantCount(ctx context.Context, id string, n int, seq int64) (Session, bool, error) {
	row, err := s.q.SetParticipantCount(ctx, gen.SetParticipantCountParams{
		ParticipantCount: int32(n),
		Seq:              seq,
		SessionID:        id,
	})
	return updatedSession(row, err, "set participant count", id)
}

// GetSession returns the session with the given id, or ErrNotFound.
func (s *Store) GetSession(ctx context.Context, id string) (Session, error) {
	row, err := s.q.GetSession(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return Session{}, ErrNotFound
	}
	if err != nil {
		return Session{}, fmt.Errorf("get session %q: %w", id, err)
	}
	return sessionFromRow(row), nil
}

// ListSessions returns up to limit sessions, live ones first and newest first
// within each group.
func (s *Store) ListSessions(ctx context.Context, limit int) ([]Session, error) {
	rows, err := s.q.ListSessions(ctx, int32(limit))
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	out := make([]Session, 0, len(rows))
	for _, r := range rows {
		out = append(out, sessionFromRow(r))
	}
	return out, nil
}

// ListIdleLiveSessions returns live sessions whose most recent event, or
// start time when they have no events, is before cutoff.
func (s *Store) ListIdleLiveSessions(ctx context.Context, cutoff time.Time) ([]Session, error) {
	rows, err := s.q.ListIdleLiveSessions(ctx, cutoff)
	if err != nil {
		return nil, fmt.Errorf("list idle sessions: %w", err)
	}
	out := make([]Session, 0, len(rows))
	for _, r := range rows {
		out = append(out, sessionFromRow(r))
	}
	return out, nil
}

// InsertEvent stores one event. inserted is false and id is 0 when
// (sessionID, seq) was already stored.
func (s *Store) InsertEvent(ctx context.Context, sessionID string, seq int, typ string, ts time.Time, data json.RawMessage) (int64, bool, error) {
	id, err := s.q.InsertEvent(ctx, gen.InsertEventParams{
		SessionID: sessionID,
		Seq:       int32(seq),
		Type:      typ,
		Ts:        ts,
		Data:      orEmptyObject(data),
	})
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("insert event %s/%d: %w", sessionID, seq, err)
	}
	return id, true, nil
}

// ListEventsBySession returns every event of a session ordered by ts, then
// seq. A session with no events yields an empty, non-nil slice.
func (s *Store) ListEventsBySession(ctx context.Context, id string) ([]Event, error) {
	rows, err := s.q.ListEventsBySession(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("list events %q: %w", id, err)
	}
	out := make([]Event, 0, len(rows))
	for _, r := range rows {
		out = append(out, eventFromRow(r))
	}
	return out, nil
}

// updatedSession turns the result of a guarded UPDATE ... RETURNING into
// (session, updated, error): no row means the guard rejected the change.
func updatedSession(row gen.Session, err error, op, id string) (Session, bool, error) {
	if errors.Is(err, sql.ErrNoRows) {
		return Session{}, false, nil
	}
	if err != nil {
		return Session{}, false, fmt.Errorf("%s %q: %w", op, id, err)
	}
	return sessionFromRow(row), true, nil
}

func sessionFromRow(r gen.Session) Session {
	s := Session{
		ID:               r.SessionID,
		SpaceID:          r.SpaceID,
		Status:           r.Status,
		StartedAt:        r.StartedAt.UTC(),
		Source:           r.Source,
		ParticipantCount: int(r.ParticipantCount),
		Metadata:         r.Metadata,
		LastWebhookSeq:   r.LastWebhookSeq,
	}
	if r.EndedAt.Valid {
		t := r.EndedAt.Time.UTC()
		s.EndedAt = &t
	}
	return s
}

func eventFromRow(r gen.Event) Event {
	return Event{
		ID:         r.ID,
		SessionID:  r.SessionID,
		Seq:        int(r.Seq),
		Type:       r.Type,
		TS:         r.Ts.UTC(),
		ReceivedAt: r.ReceivedAt.UTC(),
		Data:       r.Data,
	}
}

// orEmptyObject substitutes "{}" for absent JSON so non-null jsonb columns
// never receive NULL.
func orEmptyObject(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return emptyJSONObject
	}
	return raw
}
