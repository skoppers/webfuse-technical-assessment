package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestFindOrCreateSession(t *testing.T) {
	st := New(openTestDB(t))
	ctx := context.Background()
	started := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

	s, inserted, err := st.FindOrCreateSession(ctx, "s1", "space-a", started, SourceEvent)
	if err != nil {
		t.Fatal(err)
	}
	if !inserted {
		t.Fatal("first call: inserted=false")
	}
	if s.ID != "s1" || s.SpaceID != "space-a" || s.Status != StatusLive || s.Source != SourceEvent ||
		!s.StartedAt.Equal(started) || s.EndedAt != nil || s.ParticipantCount != 0 ||
		s.LastWebhookSeq != 0 || string(s.Metadata) != "{}" {
		t.Errorf("unexpected session after insert: %+v", s)
	}

	again, inserted, err := st.FindOrCreateSession(ctx, "s1", "other-space", started.Add(time.Hour), SourceWebhook)
	if err != nil {
		t.Fatal(err)
	}
	if inserted {
		t.Error("second call: inserted=true")
	}
	if again.SpaceID != "space-a" || again.Source != SourceEvent || !again.StartedAt.Equal(started) {
		t.Errorf("second call changed the row: %+v", again)
	}
}

func TestGetSessionNotFound(t *testing.T) {
	st := New(openTestDB(t))
	_, err := st.GetSession(context.Background(), "missing")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestEnrichSessionFromWebhook(t *testing.T) {
	st := New(openTestDB(t))
	ctx := context.Background()
	stub := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	real := stub.Add(-30 * time.Second)
	mustCreate(t, st, "s1", stub)

	s, updated, err := st.EnrichSessionFromWebhook(ctx, EnrichParams{
		ID: "s1", SpaceID: "space-w", StartedAt: real, ParticipantCount: 2,
		Metadata: json.RawMessage(`{"name":"demo"}`), Seq: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !updated {
		t.Fatal("first enrich: updated=false")
	}
	if s.SpaceID != "space-w" || !s.StartedAt.Equal(real) || s.ParticipantCount != 2 ||
		s.Source != SourceWebhook || s.LastWebhookSeq != 10 || string(s.Metadata) != `{"name": "demo"}` {
		t.Errorf("enriched session: %+v", s)
	}

	// Same seq and a lower seq are both stale.
	for _, seq := range []int64{10, 9} {
		_, updated, err := st.EnrichSessionFromWebhook(ctx, EnrichParams{ID: "s1", SpaceID: "stale", Seq: seq})
		if err != nil {
			t.Fatal(err)
		}
		if updated {
			t.Errorf("seq %d: stale enrich applied", seq)
		}
	}
	got, err := st.GetSession(ctx, "s1")
	if err != nil {
		t.Fatal(err)
	}
	if got.SpaceID != "space-w" || got.LastWebhookSeq != 10 {
		t.Errorf("stale enrich changed row: %+v", got)
	}

	// Nil metadata is stored as an empty object.
	s, updated, err = st.EnrichSessionFromWebhook(ctx, EnrichParams{ID: "s1", SpaceID: "space-w", StartedAt: real, Seq: 11})
	if err != nil || !updated {
		t.Fatalf("enrich seq 11: updated=%v err=%v", updated, err)
	}
	if string(s.Metadata) != "{}" {
		t.Errorf("metadata = %s, want {}", s.Metadata)
	}

	// Unknown session: nothing to update.
	_, updated, err = st.EnrichSessionFromWebhook(ctx, EnrichParams{ID: "nope", Seq: 1})
	if err != nil || updated {
		t.Errorf("unknown session: updated=%v err=%v", updated, err)
	}
}

func TestEnrichCannotReviveEndedSession(t *testing.T) {
	st := New(openTestDB(t))
	ctx := context.Background()
	mustCreate(t, st, "s1", time.Now())
	if _, ended, err := st.EndSession(ctx, "s1", time.Now(), 5); err != nil || !ended {
		t.Fatalf("end: ended=%v err=%v", ended, err)
	}

	_, updated, err := st.EnrichSessionFromWebhook(ctx, EnrichParams{ID: "s1", SpaceID: "x", Seq: 100})
	if err != nil {
		t.Fatal(err)
	}
	if updated {
		t.Error("enrich revived an ended session")
	}
	_, updated, err = st.SetParticipantCount(ctx, "s1", 3, 100)
	if err != nil {
		t.Fatal(err)
	}
	if updated {
		t.Error("participant count applied to an ended session")
	}
	got, err := st.GetSession(ctx, "s1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != StatusEnded || got.LastWebhookSeq != 5 || got.ParticipantCount != 0 {
		t.Errorf("ended session changed: %+v", got)
	}
}

func TestEndSession(t *testing.T) {
	st := New(openTestDB(t))
	ctx := context.Background()
	mustCreate(t, st, "s1", time.Now())
	at := time.Date(2026, 1, 2, 3, 5, 0, 0, time.UTC)

	// The reaper passes seq 0: no ordering guard.
	s, ended, err := st.EndSession(ctx, "s1", at, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !ended {
		t.Fatal("seq 0 did not end a live session")
	}
	if s.Status != StatusEnded || s.EndedAt == nil || !s.EndedAt.Equal(at) || s.LastWebhookSeq != 0 {
		t.Errorf("ended session: %+v", s)
	}

	// Already ended: no-op for any seq.
	for _, seq := range []int64{0, 7} {
		if _, ended, err := st.EndSession(ctx, "s1", at.Add(time.Minute), seq); err != nil || ended {
			t.Errorf("seq %d on ended session: ended=%v err=%v", seq, ended, err)
		}
	}
	got, err := st.GetSession(ctx, "s1")
	if err != nil {
		t.Fatal(err)
	}
	if !got.EndedAt.Equal(at) {
		t.Errorf("ended_at changed to %v", got.EndedAt)
	}

	// A webhook end raises last_webhook_seq to its own seq.
	mustCreate(t, st, "s2", time.Now())
	if _, updated, err := st.SetParticipantCount(ctx, "s2", 1, 3); err != nil || !updated {
		t.Fatalf("participants: updated=%v err=%v", updated, err)
	}
	s, ended, err = st.EndSession(ctx, "s2", at, 4)
	if err != nil || !ended {
		t.Fatalf("end seq 4: ended=%v err=%v", ended, err)
	}
	if s.LastWebhookSeq != 4 {
		t.Errorf("last_webhook_seq = %d, want 4", s.LastWebhookSeq)
	}

	// A stale seq leaves a live session alone.
	mustCreate(t, st, "s3", time.Now())
	if _, updated, err := st.SetParticipantCount(ctx, "s3", 1, 8); err != nil || !updated {
		t.Fatalf("participants: updated=%v err=%v", updated, err)
	}
	if _, ended, err := st.EndSession(ctx, "s3", at, 8); err != nil || ended {
		t.Errorf("stale end: ended=%v err=%v", ended, err)
	}
	got, err = st.GetSession(ctx, "s3")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != StatusLive || got.EndedAt != nil {
		t.Errorf("stale end changed session: %+v", got)
	}
}

func TestSetParticipantCount(t *testing.T) {
	st := New(openTestDB(t))
	ctx := context.Background()
	mustCreate(t, st, "s1", time.Now())

	s, updated, err := st.SetParticipantCount(ctx, "s1", 4, 2)
	if err != nil || !updated {
		t.Fatalf("seq 2: updated=%v err=%v", updated, err)
	}
	if s.ParticipantCount != 4 || s.LastWebhookSeq != 2 {
		t.Errorf("after seq 2: %+v", s)
	}
	if _, updated, err := st.SetParticipantCount(ctx, "s1", 9, 2); err != nil || updated {
		t.Errorf("stale seq: updated=%v err=%v", updated, err)
	}
	s, updated, err = st.SetParticipantCount(ctx, "s1", 3, 5)
	if err != nil || !updated {
		t.Fatalf("seq 5: updated=%v err=%v", updated, err)
	}
	if s.ParticipantCount != 3 || s.LastWebhookSeq != 5 {
		t.Errorf("after seq 5: %+v", s)
	}
}

func TestListSessions(t *testing.T) {
	st := New(openTestDB(t))
	ctx := context.Background()
	base := time.Date(2026, 1, 2, 3, 0, 0, 0, time.UTC)

	// Two ended (one newest overall) and two live; live must sort first.
	mustCreate(t, st, "ended-new", base.Add(3*time.Hour))
	mustCreate(t, st, "live-old", base.Add(1*time.Hour))
	mustCreate(t, st, "ended-old", base)
	mustCreate(t, st, "live-new", base.Add(2*time.Hour))
	for _, id := range []string{"ended-new", "ended-old"} {
		if _, ended, err := st.EndSession(ctx, id, base.Add(4*time.Hour), 0); err != nil || !ended {
			t.Fatalf("end %s: ended=%v err=%v", id, ended, err)
		}
	}
	mustInsertEvent(t, st, "live-old", 1)
	mustInsertEvent(t, st, "live-old", 2)
	mustInsertEvent(t, st, "ended-old", 1)

	got, err := st.ListSessions(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	wantOrder := []string{"live-new", "live-old", "ended-new", "ended-old"}
	if len(got) != len(wantOrder) {
		t.Fatalf("got %d sessions, want %d", len(got), len(wantOrder))
	}
	for i, s := range got {
		if s.ID != wantOrder[i] {
			t.Errorf("position %d: %s, want %s", i, s.ID, wantOrder[i])
		}
	}

	limited, err := st.ListSessions(ctx, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(limited) != 2 || limited[0].ID != "live-new" || limited[1].ID != "live-old" {
		t.Errorf("limit 2: %+v", limited)
	}
}

func TestListIdleLiveSessions(t *testing.T) {
	db := openTestDB(t)
	st := New(db)
	ctx := context.Background()
	now := time.Now()

	mustCreate(t, st, "idle", now.Add(-10*time.Minute))
	mustInsertEvent(t, st, "idle", 1)
	setReceivedAt(t, db, "idle", now.Add(-2*time.Minute))

	mustCreate(t, st, "active", now.Add(-10*time.Minute))
	mustInsertEvent(t, st, "active", 1)
	mustInsertEvent(t, st, "active", 2)
	setReceivedAt(t, db, "active", now.Add(-10*time.Second))

	// No events: started_at stands in for the last activity.
	mustCreate(t, st, "empty-old", now.Add(-5*time.Minute))
	mustCreate(t, st, "empty-new", now.Add(-5*time.Second))

	// Ended sessions are never idle candidates.
	mustCreate(t, st, "ended", now.Add(-10*time.Minute))
	if _, ended, err := st.EndSession(ctx, "ended", now, 0); err != nil || !ended {
		t.Fatalf("end: ended=%v err=%v", ended, err)
	}

	got, err := st.ListIdleLiveSessions(ctx, now.Add(-time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]string, 0, len(got))
	for _, s := range got {
		ids = append(ids, s.ID)
	}
	if len(ids) != 2 || ids[0] != "idle" || ids[1] != "empty-old" {
		t.Errorf("idle sessions = %v, want [idle empty-old]", ids)
	}
}

func TestInsertEvent(t *testing.T) {
	st := New(openTestDB(t))
	ctx := context.Background()
	mustCreate(t, st, "s1", time.Now())
	ts := time.UnixMilli(1767322445123).UTC() // non-zero millisecond part

	id, inserted, err := st.InsertEvent(ctx, "s1", 1, "click", ts, json.RawMessage(`{"x":1}`))
	if err != nil {
		t.Fatal(err)
	}
	if !inserted || id == 0 {
		t.Fatalf("first insert: id=%d inserted=%v", id, inserted)
	}

	dupID, inserted, err := st.InsertEvent(ctx, "s1", 1, "click", ts.Add(5*time.Millisecond), json.RawMessage(`{"x":2}`))
	if err != nil {
		t.Fatal(err)
	}
	if inserted || dupID != 0 {
		t.Errorf("duplicate insert: id=%d inserted=%v", dupID, inserted)
	}

	// Nil data is stored as an empty object.
	if _, inserted, err := st.InsertEvent(ctx, "s1", 2, "scroll", ts, nil); err != nil || !inserted {
		t.Fatalf("insert nil data: inserted=%v err=%v", inserted, err)
	}

	events, err := st.ListEventsBySession(ctx, "s1")
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Fatalf("got %d events, want 2", len(events))
	}
	first := events[0]
	if first.ID != id || first.SessionID != "s1" || first.Seq != 1 || first.Type != "click" ||
		!first.TS.Equal(ts) || string(first.Data) != `{"x": 1}` || first.ReceivedAt.IsZero() {
		t.Errorf("first event: %+v", first)
	}
	if string(events[1].Data) != "{}" {
		t.Errorf("nil data stored as %s", events[1].Data)
	}

	// Unknown session violates the foreign key.
	if _, _, err := st.InsertEvent(ctx, "missing", 1, "click", ts, nil); err == nil {
		t.Error("insert for unknown session succeeded")
	}
}

func TestListAndCountEventsBySession(t *testing.T) {
	st := New(openTestDB(t))
	ctx := context.Background()
	mustCreate(t, st, "s1", time.Now())
	mustCreate(t, st, "s2", time.Now())
	base := time.UnixMilli(1767322445000).UTC()

	// Inserted out of order; two events share a ts so seq breaks the tie.
	for _, e := range []struct {
		seq int
		ts  time.Time
	}{{3, base.Add(2 * time.Second)}, {1, base.Add(time.Second)}, {4, base.Add(time.Second)}, {2, base}} {
		if _, inserted, err := st.InsertEvent(ctx, "s1", e.seq, "click", e.ts, nil); err != nil || !inserted {
			t.Fatalf("insert seq %d: inserted=%v err=%v", e.seq, inserted, err)
		}
	}
	mustInsertEvent(t, st, "s2", 1)

	events, err := st.ListEventsBySession(ctx, "s1")
	if err != nil {
		t.Fatal(err)
	}
	wantSeq := []int{2, 1, 4, 3}
	if len(events) != len(wantSeq) {
		t.Fatalf("got %d events, want %d", len(events), len(wantSeq))
	}
	for i, e := range events {
		if e.Seq != wantSeq[i] {
			t.Errorf("position %d: seq %d, want %d", i, e.Seq, wantSeq[i])
		}
	}

	n, err := st.CountEventsBySession(ctx, "s1")
	if err != nil {
		t.Fatal(err)
	}
	if n != 4 {
		t.Errorf("count = %d, want 4", n)
	}

	none, err := st.ListEventsBySession(ctx, "nobody")
	if err != nil {
		t.Fatal(err)
	}
	if none == nil || len(none) != 0 {
		t.Errorf("events for unknown session = %#v, want empty slice", none)
	}
	n, err = st.CountEventsBySession(ctx, "nobody")
	if err != nil || n != 0 {
		t.Errorf("count for unknown session = %d, %v", n, err)
	}
}

func mustCreate(t *testing.T, st *Store, id string, startedAt time.Time) {
	t.Helper()
	if _, inserted, err := st.FindOrCreateSession(context.Background(), id, "space", startedAt, SourceEvent); err != nil || !inserted {
		t.Fatalf("create %s: inserted=%v err=%v", id, inserted, err)
	}
}

func mustInsertEvent(t *testing.T, st *Store, sessionID string, seq int) {
	t.Helper()
	if _, inserted, err := st.InsertEvent(context.Background(), sessionID, seq, "click", time.Now(), nil); err != nil || !inserted {
		t.Fatalf("insert event %s/%d: inserted=%v err=%v", sessionID, seq, inserted, err)
	}
}

// setReceivedAt backdates every event of a session so idle detection can be
// tested without waiting.
func setReceivedAt(t *testing.T, db *sql.DB, sessionID string, at time.Time) {
	t.Helper()
	if _, err := db.ExecContext(context.Background(),
		"UPDATE events SET received_at = $1 WHERE session_id = $2", at, sessionID); err != nil {
		t.Fatalf("set received_at for %s: %v", sessionID, err)
	}
}
