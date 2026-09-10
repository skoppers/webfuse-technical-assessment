package webhook

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/skoppers/webfuse-activity-analyzer/server/internal/event"
	"github.com/skoppers/webfuse-activity-analyzer/server/internal/session"
	"github.com/skoppers/webfuse-activity-analyzer/server/internal/store"
	"github.com/skoppers/webfuse-activity-analyzer/server/internal/stream"
)

// fixture is the webhook handler over a real Lifecycle on a clean test
// database, with one per-session subscription on the hub for sessionID.
type fixture struct {
	h   http.Handler
	lc  *session.Lifecycle
	one <-chan stream.Message
}

// newFixture opens TEST_DATABASE_URL, migrates, truncates and subscribes.
// It skips the test when TEST_DATABASE_URL is unset.
func newFixture(t *testing.T) *fixture {
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
	one, cancel := hub.Subscribe(stream.Filter{SessionID: sessionID})
	t.Cleanup(cancel)
	lc := session.New(store.New(db), hub)
	return &fixture{h: Handler(lc, testKey), lc: lc, one: one}
}

// post gzips body, signs the plain bytes with key (hex HMAC) and sends it to
// POST /webfuse.
func post(t *testing.T, h http.Handler, key string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/webfuse", bytes.NewReader(gz(t, body)))
	req.Header.Set("Content-Encoding", "gzip")
	req.Header.Set("Webhook-Signature", hex.EncodeToString(sign(key, body)))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// withSeq returns payload with its sequence_id replaced.
func withSeq(t *testing.T, payload string, seq int64) []byte {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(payload), &m); err != nil {
		t.Fatal(err)
	}
	m["sequence_id"] = seq
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// outcome asserts a 200 and returns the outcome from the body.
func outcome(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
	}
	var r response
	if err := json.Unmarshal(rec.Body.Bytes(), &r); err != nil {
		t.Fatalf("body %q: %v", rec.Body, err)
	}
	return r.Outcome
}

// get returns the session row for sessionID.
func (f *fixture) get(t *testing.T) session.Session {
	t.Helper()
	s, err := f.lc.Get(context.Background(), sessionID)
	if err != nil {
		t.Fatalf("get %s: %v", sessionID, err)
	}
	return s
}

// assertNoRow asserts no session row exists for id.
func (f *fixture) assertNoRow(t *testing.T, id string) {
	t.Helper()
	if _, err := f.lc.Get(context.Background(), id); !errors.Is(err, session.ErrNotFound) {
		t.Fatalf("get %s: err = %v, want ErrNotFound", id, err)
	}
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

func TestStartedCreatesSession(t *testing.T) {
	f := newFixture(t)

	if got := outcome(t, post(t, f.h, testKey, []byte(payloadStarted))); got != OutcomeApplied {
		t.Fatalf("outcome = %q, want applied", got)
	}
	s := f.get(t)
	wantStart := time.Date(2025, 2, 7, 17, 6, 51, 184169000, time.UTC)
	if s.Status != session.StatusLive || s.Source != store.SourceWebhook || s.SpaceID != "23" ||
		s.ParticipantCount != 0 || !s.StartedAt.Equal(wantStart) || s.LastWebhookSeq != 40 {
		t.Errorf("session: %+v", s)
	}
	if !strings.Contains(string(s.Metadata), `"link"`) {
		t.Errorf("metadata = %s, want the raw webhook data", s.Metadata)
	}
	if p := sessionPayload(t, recv(t, f.one)); p.SessionID != sessionID || p.SpaceID != "23" || p.Status != session.StatusLive {
		t.Errorf("payload: %+v", p)
	}
	assertNone(t, f.one)
}

func TestJoinedAppliesOnceThenStale(t *testing.T) {
	f := newFixture(t)
	outcome(t, post(t, f.h, testKey, []byte(payloadStarted)))
	recv(t, f.one)

	if got := outcome(t, post(t, f.h, testKey, []byte(payloadJoined))); got != OutcomeApplied {
		t.Fatalf("joined outcome = %q, want applied", got)
	}
	if s := f.get(t); s.ParticipantCount != 2 || s.LastWebhookSeq != 56 {
		t.Errorf("after joined: %+v", s)
	}
	if p := sessionPayload(t, recv(t, f.one)); p.ParticipantCount != 2 {
		t.Errorf("joined payload: %+v", p)
	}
	assertNone(t, f.one)

	// Same delivery again: same seq, nothing changes, nothing published.
	if got := outcome(t, post(t, f.h, testKey, []byte(payloadJoined))); got != OutcomeStale {
		t.Fatalf("replayed joined outcome = %q, want stale", got)
	}
	if s := f.get(t); s.ParticipantCount != 2 {
		t.Errorf("after replay: %+v", s)
	}
	assertNone(t, f.one)
}

func TestLeftDecrementsAndFloors(t *testing.T) {
	f := newFixture(t)
	outcome(t, post(t, f.h, testKey, []byte(payloadStarted)))
	outcome(t, post(t, f.h, testKey, []byte(payloadJoined)))
	recv(t, f.one)
	recv(t, f.one)

	if got := outcome(t, post(t, f.h, testKey, []byte(payloadLeft))); got != OutcomeApplied {
		t.Fatalf("left 57 outcome = %q, want applied", got)
	}
	if s := f.get(t); s.ParticipantCount != 1 || s.LastWebhookSeq != 57 {
		t.Errorf("after left 57: %+v", s)
	}
	if p := sessionPayload(t, recv(t, f.one)); p.ParticipantCount != 1 {
		t.Errorf("left payload: %+v", p)
	}

	if got := outcome(t, post(t, f.h, testKey, withSeq(t, payloadLeft, 58))); got != OutcomeApplied {
		t.Fatalf("left 58 outcome = %q, want applied", got)
	}
	if s := f.get(t); s.ParticipantCount != 0 {
		t.Errorf("after left 58: %+v", s)
	}
	recv(t, f.one)

	// Below zero floors at 0. The row still advances its seq, so the store
	// reports a change and the outcome is applied.
	if got := outcome(t, post(t, f.h, testKey, withSeq(t, payloadLeft, 59))); got != OutcomeApplied {
		t.Fatalf("left 59 outcome = %q, want applied", got)
	}
	if s := f.get(t); s.ParticipantCount != 0 || s.LastWebhookSeq != 59 {
		t.Errorf("after left 59: %+v", s)
	}
	recv(t, f.one)
	assertNone(t, f.one)

	// Unknown session: nothing to decrement, no stub created.
	unknown := strings.Replace(payloadLeft, sessionID, "never-seen", 1)
	if got := outcome(t, post(t, f.h, testKey, []byte(unknown))); got != OutcomeIgnored {
		t.Fatalf("left unknown outcome = %q, want ignored", got)
	}
	f.assertNoRow(t, "never-seen")
	assertNone(t, f.one)
}

func TestEndedThenStartedDoesNotRevive(t *testing.T) {
	f := newFixture(t)
	outcome(t, post(t, f.h, testKey, []byte(payloadStarted)))
	recv(t, f.one)

	if got := outcome(t, post(t, f.h, testKey, []byte(payloadEnded))); got != OutcomeApplied {
		t.Fatalf("ended outcome = %q, want applied", got)
	}
	wantEnd := time.Date(2025, 2, 7, 17, 16, 51, 197164000, time.UTC)
	s := f.get(t)
	if s.Status != session.StatusEnded || s.EndedAt == nil || !s.EndedAt.Equal(wantEnd) || s.LastWebhookSeq != 53 {
		t.Errorf("after ended: %+v", s)
	}
	if p := sessionPayload(t, recv(t, f.one)); p.Status != session.StatusEnded || p.EndedAt == nil || !p.EndedAt.Equal(wantEnd) {
		t.Errorf("ended payload: %+v", p)
	}
	assertNone(t, f.one)

	// A replayed started (seq 40) after the end is stale and cannot revive it.
	if got := outcome(t, post(t, f.h, testKey, []byte(payloadStarted))); got != OutcomeStale {
		t.Fatalf("replayed started outcome = %q, want stale", got)
	}
	if s := f.get(t); s.Status != session.StatusEnded {
		t.Errorf("revived: %+v", s)
	}
	assertNone(t, f.one)
}

func TestUnknownCategoryIgnored(t *testing.T) {
	f := newFixture(t)
	body := withSeq(t, strings.Replace(payloadStarted, CategorySessionStarted, "space.session.screenshot", 1), 60)
	if got := outcome(t, post(t, f.h, testKey, body)); got != OutcomeIgnored {
		t.Fatalf("outcome = %q, want ignored", got)
	}
	f.assertNoRow(t, sessionID)
	assertNone(t, f.one)
}

func TestWrongSignatureRejected(t *testing.T) {
	f := newFixture(t)
	rec := post(t, f.h, "wrong", []byte(payloadStarted))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
	}
	f.assertNoRow(t, sessionID)
	assertNone(t, f.one)
}

func TestMalformedEnvelopeRejected(t *testing.T) {
	f := newFixture(t)
	body := strings.Replace(payloadStarted, `"category":"space.session.started",`, "", 1)
	rec := post(t, f.h, testKey, []byte(body))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
	}
	var m map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil || !strings.Contains(m["error"], "category") {
		t.Errorf("body = %s, want an error naming category", rec.Body)
	}
	f.assertNoRow(t, sessionID)
	assertNone(t, f.one)
}

func TestRoundTripWithIngestedEvents(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	if got := outcome(t, post(t, f.h, testKey, []byte(payloadStarted))); got != OutcomeApplied {
		t.Fatalf("started outcome = %q", got)
	}
	ts := time.Date(2025, 2, 7, 17, 9, 0, 0, time.UTC)
	res, err := f.lc.RecordEvents(ctx, sessionID, "23", ts, []session.NewEvent{
		{Type: event.Click, Seq: 1, TS: ts},
		{Type: event.FormSubmit, Seq: 2, TS: ts.Add(time.Second)},
	})
	if err != nil || res.Inserted != 2 {
		t.Fatalf("record events: %+v err=%v", res, err)
	}
	if got := outcome(t, post(t, f.h, testKey, []byte(payloadEnded))); got != OutcomeApplied {
		t.Fatalf("ended outcome = %q", got)
	}

	s := f.get(t)
	if s.Status != session.StatusEnded || s.EndedAt == nil {
		t.Errorf("final row: %+v", s)
	}
	events, err := f.lc.Events(ctx, sessionID)
	if err != nil || len(events) != 2 {
		t.Errorf("events = %d err=%v, want 2", len(events), err)
	}

	// Per-session stream: session (started), activity x2, session (ended).
	if p := sessionPayload(t, recv(t, f.one)); p.Status != session.StatusLive {
		t.Errorf("first payload: %+v", p)
	}
	for i := range 2 {
		if m := recv(t, f.one); m.Event != stream.EventActivity {
			t.Errorf("message %d = %+v, want activity", i+1, m)
		}
	}
	if p := sessionPayload(t, recv(t, f.one)); p.Status != session.StatusEnded {
		t.Errorf("last payload: %+v", p)
	}
	assertNone(t, f.one)
}
