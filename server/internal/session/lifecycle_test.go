package session

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/skoppers/webfuse-activity-analyzer/server/internal/event"
	"github.com/skoppers/webfuse-activity-analyzer/server/internal/store"
	"github.com/skoppers/webfuse-activity-analyzer/server/internal/stream"
)

// fixture is a Lifecycle over a clean test database with an overview
// subscription and one per-session subscription on the hub.
type fixture struct {
	db       *sql.DB
	lc       *Lifecycle
	hub      *stream.Hub
	overview <-chan stream.Message
	one      <-chan stream.Message
}

// newFixture opens TEST_DATABASE_URL, migrates, truncates and subscribes.
// It skips the test when TEST_DATABASE_URL is unset.
func newFixture(t *testing.T, sessionID string) *fixture {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	db, err := store.Open(ctx, url)
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := store.Migrate(ctx, db); err != nil {
		t.Fatalf("migrate test db: %v", err)
	}
	if _, err := db.ExecContext(ctx, "TRUNCATE events, sessions CASCADE"); err != nil {
		t.Fatalf("truncate test db: %v", err)
	}
	hub := stream.NewHub()
	overview, cancelO := hub.Subscribe(stream.Filter{})
	one, cancelS := hub.Subscribe(stream.Filter{SessionID: sessionID})
	t.Cleanup(func() { cancelO(); cancelS() })
	return &fixture{db: db, lc: New(store.New(db), hub), hub: hub, overview: overview, one: one}
}

func recv(t *testing.T, ch <-chan stream.Message) stream.Message {
	t.Helper()
	select {
	case m, ok := <-ch:
		if !ok {
			t.Fatal("channel closed")
		}
		return m
	case <-time.After(time.Second):
		t.Fatal("no message within 1s")
	}
	return stream.Message{}
}

func assertNone(t *testing.T, ch <-chan stream.Message) {
	t.Helper()
	select {
	case m, ok := <-ch:
		if !ok {
			t.Fatal("channel closed")
		}
		t.Fatalf("unexpected message %+v", m)
	default:
	}
}

func sessionPayload(t *testing.T, m stream.Message) stream.SessionPayload {
	t.Helper()
	if m.Event != stream.EventSession {
		t.Fatalf("event = %q, want session (%+v)", m.Event, m)
	}
	p, ok := m.Data.(stream.SessionPayload)
	if !ok {
		t.Fatalf("data is %T, want SessionPayload", m.Data)
	}
	return p
}

func activityPayload(t *testing.T, m stream.Message) stream.ActivityPayload {
	t.Helper()
	if m.Event != stream.EventActivity {
		t.Fatalf("event = %q, want activity (%+v)", m.Event, m)
	}
	p, ok := m.Data.(stream.ActivityPayload)
	if !ok {
		t.Fatalf("data is %T, want ActivityPayload", m.Data)
	}
	return p
}

func TestStartedCreatesAndPublishes(t *testing.T) {
	f := newFixture(t, "s1")
	ctx := context.Background()
	at := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

	s, changed, err := f.lc.Started(ctx, StartedParams{
		ID: "s1", SpaceID: "sp", StartedAt: at, ParticipantCount: 2,
		Metadata: json.RawMessage(`{"name":"demo"}`), Seq: 10,
	})
	if err != nil || !changed {
		t.Fatalf("started: changed=%v err=%v", changed, err)
	}
	if s.ID != "s1" || s.Status != StatusLive || s.Source != store.SourceWebhook ||
		s.ParticipantCount != 2 || s.LastWebhookSeq != 10 || !s.StartedAt.Equal(at) {
		t.Errorf("session: %+v", s)
	}

	// Created and enriched in one call: exactly one session message.
	for name, ch := range map[string]<-chan stream.Message{"overview": f.overview, "s1": f.one} {
		p := sessionPayload(t, recv(t, ch))
		if p.SessionID != "s1" || p.SpaceID != "sp" || p.Status != StatusLive || p.ParticipantCount != 2 || p.EndedAt != nil {
			t.Errorf("%s payload: %+v", name, p)
		}
		assertNone(t, ch)
	}

	// Stale seq: nothing changes, nothing published.
	again, changed, err := f.lc.Started(ctx, StartedParams{ID: "s1", SpaceID: "other", StartedAt: at, Seq: 10})
	if err != nil || changed {
		t.Fatalf("stale started: changed=%v err=%v", changed, err)
	}
	if again.SpaceID != "sp" || again.LastWebhookSeq != 10 {
		t.Errorf("stale started changed row: %+v", again)
	}
	assertNone(t, f.overview)
	assertNone(t, f.one)

	// Newer seq enriches and publishes again.
	if _, changed, err := f.lc.Started(ctx, StartedParams{ID: "s1", SpaceID: "sp2", StartedAt: at, ParticipantCount: 3, Seq: 11}); err != nil || !changed {
		t.Fatalf("re-enrich: changed=%v err=%v", changed, err)
	}
	if p := sessionPayload(t, recv(t, f.overview)); p.SpaceID != "sp2" || p.ParticipantCount != 3 {
		t.Errorf("re-enriched payload: %+v", p)
	}
	recv(t, f.one)
}

func TestStartedEnrichesEventStub(t *testing.T) {
	f := newFixture(t, "s1")
	ctx := context.Background()
	ts := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	now := ts.Add(time.Minute)

	// First event creates a stub (source event); a late started webhook enriches it.
	if _, err := f.lc.RecordEvents(ctx, "s1", "sp", now, []NewEvent{{Type: event.Click, Seq: 1, TS: ts}}); err != nil {
		t.Fatal(err)
	}
	if p := sessionPayload(t, recv(t, f.overview)); p.SpaceID != "sp" || p.ParticipantCount != 0 {
		t.Errorf("stub payload: %+v", p)
	}
	assertNone(t, f.overview) // click is not a key event
	recv(t, f.one)            // stub
	recv(t, f.one)            // click

	s, _, err := f.lc.Started(ctx, StartedParams{ID: "s1", SpaceID: "sp-real", StartedAt: ts.Add(-time.Second), ParticipantCount: 2, Seq: 1})
	if err != nil {
		t.Fatal(err)
	}
	if s.Source != store.SourceWebhook || s.SpaceID != "sp-real" || s.ParticipantCount != 2 || !s.StartedAt.Equal(ts.Add(-time.Second)) {
		t.Errorf("enriched stub: %+v", s)
	}
	if p := sessionPayload(t, recv(t, f.overview)); p.SpaceID != "sp-real" || p.ParticipantCount != 2 {
		t.Errorf("enriched payload: %+v", p)
	}
	assertNone(t, f.overview)
}

func TestEndedPublishesOnce(t *testing.T) {
	f := newFixture(t, "s1")
	ctx := context.Background()
	at := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

	// Unknown session: nothing to end, nothing published.
	if _, ended, err := f.lc.Ended(ctx, "s1", at, 1); err != nil || ended {
		t.Fatalf("end unknown: ended=%v err=%v", ended, err)
	}
	assertNone(t, f.overview)

	if _, _, err := f.lc.Started(ctx, StartedParams{ID: "s1", SpaceID: "sp", StartedAt: at, Seq: 1}); err != nil {
		t.Fatal(err)
	}
	recv(t, f.overview)
	recv(t, f.one)

	endAt := at.Add(time.Minute)
	s, ended, err := f.lc.Ended(ctx, "s1", endAt, 2)
	if err != nil || !ended {
		t.Fatalf("end: ended=%v err=%v", ended, err)
	}
	if s.Status != StatusEnded || s.EndedAt == nil || !s.EndedAt.Equal(endAt) {
		t.Errorf("ended session: %+v", s)
	}
	p := sessionPayload(t, recv(t, f.overview))
	if p.Status != StatusEnded || p.EndedAt == nil || !p.EndedAt.Equal(endAt) {
		t.Errorf("ended payload: %+v", p)
	}
	recv(t, f.one)

	// Replayed or subsequent end: no-op, no message.
	for _, seq := range []int64{2, 3} {
		if _, ended, err := f.lc.Ended(ctx, "s1", endAt.Add(time.Hour), seq); err != nil || ended {
			t.Errorf("seq %d: ended=%v err=%v", seq, ended, err)
		}
	}
	assertNone(t, f.overview)
	assertNone(t, f.one)

	// A started webhook after the end cannot revive it.
	if _, changed, err := f.lc.Started(ctx, StartedParams{ID: "s1", SpaceID: "sp", StartedAt: at, Seq: 9}); err != nil || changed {
		t.Fatalf("started after end: changed=%v err=%v", changed, err)
	}
	assertNone(t, f.overview)
}

func TestParticipantsChangedCreatesStub(t *testing.T) {
	f := newFixture(t, "s1")
	ctx := context.Background()
	at := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

	s, changed, err := f.lc.ParticipantsChanged(ctx, ParticipantsParams{ID: "s1", SpaceID: "sp", Count: 2, At: at, Seq: 5})
	if err != nil || !changed {
		t.Fatalf("participants on unknown: changed=%v err=%v", changed, err)
	}
	if s.Source != store.SourceWebhook || s.ParticipantCount != 2 || s.LastWebhookSeq != 5 || !s.StartedAt.Equal(at) {
		t.Errorf("stub: %+v", s)
	}
	if p := sessionPayload(t, recv(t, f.overview)); p.ParticipantCount != 2 || p.Status != StatusLive {
		t.Errorf("payload: %+v", p)
	}
	recv(t, f.one)
	assertNone(t, f.overview)

	// Stale seq: no change, no message.
	if _, changed, err := f.lc.ParticipantsChanged(ctx, ParticipantsParams{ID: "s1", SpaceID: "sp", Count: 9, At: at, Seq: 5}); err != nil || changed {
		t.Errorf("stale: changed=%v err=%v", changed, err)
	}
	assertNone(t, f.overview)

	// A subsequent started webhook still enriches the stub.
	if _, _, err := f.lc.Started(ctx, StartedParams{ID: "s1", SpaceID: "sp", StartedAt: at.Add(-time.Second), ParticipantCount: 3, Seq: 6}); err != nil {
		t.Fatal(err)
	}
	if p := sessionPayload(t, recv(t, f.overview)); p.ParticipantCount != 3 {
		t.Errorf("enriched stub: %+v", p)
	}
}

func TestRecordEventsDedupsAndPublishesNewOnly(t *testing.T) {
	f := newFixture(t, "s1")
	ctx := context.Background()
	base := time.UnixMilli(1767322445000).UTC()
	now := base.Add(time.Minute)

	res, err := f.lc.RecordEvents(ctx, "s1", "sp", now, []NewEvent{
		{Type: event.Click, Seq: 2, TS: base.Add(time.Second), Data: json.RawMessage(`{"tag":"button"}`)},
		{Type: event.FormSubmit, Seq: 3, TS: base.Add(2 * time.Second), Data: json.RawMessage(`{"fieldCount":3}`)},
		{Type: event.Scroll, Seq: 1, TS: base},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Inserted != 3 || res.Duplicates != 0 {
		t.Errorf("result: %+v", res)
	}
	if res.Session.ID != "s1" || res.Session.Source != store.SourceEvent || res.Session.Status != StatusLive ||
		!res.Session.StartedAt.Equal(now) {
		t.Errorf("stub session should start at the server receive time: %+v", res.Session)
	}

	// Per-session stream: stub session message, then every event in batch order.
	if p := sessionPayload(t, recv(t, f.one)); p.SessionID != "s1" || p.SpaceID != "sp" {
		t.Errorf("stub payload: %+v", p)
	}
	wantSeq := []int{2, 3, 1}
	wantKey := []bool{false, true, false}
	for i := range wantSeq {
		m := recv(t, f.one)
		p := activityPayload(t, m)
		if p.Seq != wantSeq[i] || m.Key != wantKey[i] || p.SessionID != "s1" {
			t.Errorf("event %d: key=%v payload=%+v", i, m.Key, p)
		}
		if p.Seq == 1 && string(p.Data) != "{}" {
			t.Errorf("empty data sent as %s", p.Data)
		}
		if p.Seq == 3 && (p.Type != "form_submit" || p.TS != base.Add(2*time.Second).UnixMilli()) {
			t.Errorf("key event payload: %+v", p)
		}
	}
	assertNone(t, f.one)

	// Overview: the session message and only the key event.
	sessionPayload(t, recv(t, f.overview))
	if m := recv(t, f.overview); !m.Key || activityPayload(t, m).Seq != 3 {
		t.Errorf("overview activity: %+v", m)
	}
	assertNone(t, f.overview)

	// Retry of the same batch plus one new event: only the new one is stored and published.
	res, err = f.lc.RecordEvents(ctx, "s1", "sp", now, []NewEvent{
		{Type: event.Click, Seq: 2, TS: base.Add(time.Second)},
		{Type: event.FormSubmit, Seq: 3, TS: base.Add(2 * time.Second)},
		{Type: event.Navigation, Seq: 4, TS: base.Add(3 * time.Second), Data: json.RawMessage(`{"url":"/x"}`)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Inserted != 1 || res.Duplicates != 2 {
		t.Errorf("retry result: %+v", res)
	}
	if p := activityPayload(t, recv(t, f.one)); p.Seq != 4 || p.Type != "navigation" {
		t.Errorf("retry published: %+v", p)
	}
	assertNone(t, f.one)
	assertNone(t, f.overview)

	events, err := f.lc.Events(ctx, "s1")
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 4 {
		t.Errorf("stored %d events, want 4", len(events))
	}
}

func TestRecordEventsRejectsInvalid(t *testing.T) {
	f := newFixture(t, "s1")
	ctx := context.Background()
	ts := time.Now()
	now := ts

	_, err := f.lc.RecordEvents(ctx, "s1", "sp", now, []NewEvent{
		{Type: event.Click, Seq: 1, TS: ts},
		{Type: "mousemove", Seq: 2, TS: ts},
	})
	if !errors.Is(err, ErrInvalidEventType) {
		t.Fatalf("err = %v, want ErrInvalidEventType", err)
	}
	if _, err := f.lc.RecordEvents(ctx, "s1", "sp", now, nil); !errors.Is(err, ErrEmptyBatch) {
		t.Fatalf("empty: err = %v, want ErrEmptyBatch", err)
	}
	// Whole batch rejected: no session, no events, no messages.
	if _, err := f.lc.Get(ctx, "s1"); !errors.Is(err, ErrNotFound) {
		t.Errorf("session created despite rejected batch: %v", err)
	}
	assertNone(t, f.overview)
	assertNone(t, f.one)
}

func TestLateEventsOnEndedSessionAreStoredNotRevived(t *testing.T) {
	f := newFixture(t, "s1")
	ctx := context.Background()
	at := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	now := at.Add(2 * time.Minute)

	if _, _, err := f.lc.Started(ctx, StartedParams{ID: "s1", SpaceID: "sp", StartedAt: at, Seq: 1}); err != nil {
		t.Fatal(err)
	}
	if _, ended, err := f.lc.Ended(ctx, "s1", at.Add(time.Minute), 2); err != nil || !ended {
		t.Fatalf("end: ended=%v err=%v", ended, err)
	}
	recv(t, f.overview)
	recv(t, f.overview)
	recv(t, f.one)
	recv(t, f.one)

	res, err := f.lc.RecordEvents(ctx, "s1", "sp", now, []NewEvent{{Type: event.SensitiveURL, Seq: 1, TS: at.Add(30 * time.Second)}})
	if err != nil {
		t.Fatal(err)
	}
	if res.Inserted != 1 || res.Session.Status != StatusEnded {
		t.Errorf("late event result: %+v", res)
	}
	// Activity is published; no session message, and the row stays ended.
	if m := recv(t, f.one); !m.Key || activityPayload(t, m).Type != "sensitive_url" {
		t.Errorf("late activity: %+v", m)
	}
	assertNone(t, f.one)
	if m := recv(t, f.overview); m.Event != stream.EventActivity {
		t.Errorf("overview got %+v, want key activity only", m)
	}
	assertNone(t, f.overview)
	s, err := f.lc.Get(ctx, "s1")
	if err != nil {
		t.Fatal(err)
	}
	if s.Status != StatusEnded {
		t.Errorf("session revived: %+v", s)
	}
}

func TestReapIdle(t *testing.T) {
	f := newFixture(t, "idle")
	ctx := context.Background()
	now := time.Date(2026, 1, 2, 3, 10, 0, 0, time.UTC)

	// idle: one event, backdated two minutes.
	if _, err := f.lc.RecordEvents(ctx, "idle", "sp", now.Add(-10*time.Minute), []NewEvent{{Type: event.Click, Seq: 1, TS: now.Add(-10 * time.Minute)}}); err != nil {
		t.Fatal(err)
	}
	setReceivedAt(t, f.db, "idle", now.Add(-2*time.Minute))
	// active: event ten seconds ago.
	if _, err := f.lc.RecordEvents(ctx, "active", "sp", now.Add(-10*time.Minute), []NewEvent{{Type: event.Click, Seq: 1, TS: now.Add(-10 * time.Minute)}}); err != nil {
		t.Fatal(err)
	}
	setReceivedAt(t, f.db, "active", now.Add(-10*time.Second))
	// empty-old: no events, started five minutes ago.
	if _, _, err := f.lc.Started(ctx, StartedParams{ID: "empty-old", SpaceID: "sp", StartedAt: now.Add(-5 * time.Minute), Seq: 1}); err != nil {
		t.Fatal(err)
	}
	// already ended: never a candidate.
	if _, _, err := f.lc.Started(ctx, StartedParams{ID: "done", SpaceID: "sp", StartedAt: now.Add(-20 * time.Minute), Seq: 1}); err != nil {
		t.Fatal(err)
	}
	if _, ended, err := f.lc.Ended(ctx, "done", now.Add(-15*time.Minute), 2); err != nil || !ended {
		t.Fatalf("end done: ended=%v err=%v", ended, err)
	}
	drain(f.overview) // setup messages are not under test
	drain(f.one)

	ended, err := f.lc.ReapIdle(ctx, now, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]string, 0, len(ended))
	for _, s := range ended {
		ids = append(ids, s.ID)
		if s.Status != StatusEnded || s.EndedAt == nil || !s.EndedAt.Equal(now) {
			t.Errorf("reaped %s: %+v", s.ID, s)
		}
	}
	if len(ids) != 2 || ids[0] != "idle" || ids[1] != "empty-old" {
		t.Errorf("reaped = %v, want [idle empty-old]", ids)
	}

	// One session message per reaped session, in the same order.
	for _, want := range []string{"idle", "empty-old"} {
		p := sessionPayload(t, recv(t, f.overview))
		if p.SessionID != want || p.Status != StatusEnded || p.EndedAt == nil || !p.EndedAt.Equal(now) {
			t.Errorf("reap payload: %+v, want %s ended", p, want)
		}
	}
	assertNone(t, f.overview)
	if p := sessionPayload(t, recv(t, f.one)); p.SessionID != "idle" {
		t.Errorf("per-session reap payload: %+v", p)
	}
	assertNone(t, f.one)

	// Nothing left to reap.
	if again, err := f.lc.ReapIdle(ctx, now, time.Minute); err != nil || len(again) != 0 {
		t.Errorf("second reap: %v %v", again, err)
	}
	assertNone(t, f.overview)
}

func TestReads(t *testing.T) {
	f := newFixture(t, "s1")
	ctx := context.Background()
	at := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

	if _, err := f.lc.Get(ctx, "s1"); !errors.Is(err, ErrNotFound) {
		t.Errorf("get unknown: %v", err)
	}
	if _, _, err := f.lc.Started(ctx, StartedParams{ID: "s1", SpaceID: "sp", StartedAt: at, Seq: 1}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.lc.Started(ctx, StartedParams{ID: "s2", SpaceID: "sp", StartedAt: at.Add(time.Hour), Seq: 1}); err != nil {
		t.Fatal(err)
	}
	list, err := f.lc.List(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[0].ID != "s2" || list[1].ID != "s1" {
		t.Errorf("list: %+v", list)
	}
	events, err := f.lc.Events(ctx, "s1")
	if err != nil {
		t.Fatal(err)
	}
	if events == nil || len(events) != 0 {
		t.Errorf("events of empty session: %#v", events)
	}
}

func drain(ch <-chan stream.Message) {
	for {
		select {
		case <-ch:
		default:
			return
		}
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
