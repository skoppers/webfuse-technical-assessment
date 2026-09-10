package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/skoppers/webfuse-activity-analyzer/server/internal/session"
	"github.com/skoppers/webfuse-activity-analyzer/server/internal/store"
	"github.com/skoppers/webfuse-activity-analyzer/server/internal/stream"
)

// Seeded state: live "s-live" started at liveStart with four events stored
// out of order, and ended "s-ended" started earlier with no events.
const (
	liveID  = "s-live"
	endedID = "s-ended"
)

var (
	base       = time.UnixMilli(1767322445000).UTC()
	endedStart = base.Add(-time.Hour)
	endedAt    = base.Add(-30 * time.Minute)
	liveStart  = base.Add(-time.Minute)
)

// wantSeq is the event order the store returns for s-live: by ts, then seq.
var wantSeq = []int{2, 1, 4, 3, 6, 5}

// fixture is the read API over a Lifecycle on a clean, seeded test database.
type fixture struct {
	h http.Handler
}

// newFixture opens TEST_DATABASE_URL, migrates, truncates and seeds through
// the lifecycle. It skips the test when TEST_DATABASE_URL is unset.
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
	lc := session.New(store.New(db), stream.NewHub())
	seed(t, lc)
	return &fixture{h: Handler(lc)}
}

func seed(t *testing.T, lc *session.Lifecycle) {
	t.Helper()
	ctx := context.Background()
	if _, _, err := lc.Started(ctx, session.StartedParams{
		ID: endedID, SpaceID: "sp", StartedAt: endedStart, ParticipantCount: 1, Seq: 1,
	}); err != nil {
		t.Fatal(err)
	}
	if _, ended, err := lc.Ended(ctx, endedID, endedAt, 2); err != nil || !ended {
		t.Fatalf("end %s: ended=%v err=%v", endedID, ended, err)
	}
	if _, _, err := lc.Started(ctx, session.StartedParams{
		ID: liveID, SpaceID: "sp", StartedAt: liveStart, ParticipantCount: 2,
		Metadata: json.RawMessage(`{"plan":"pro"}`), Seq: 3,
	}); err != nil {
		t.Fatal(err)
	}
	// Out of order by seq and ts; seq 1 and 4 share a ts so seq breaks the tie.
	events := []session.NewEvent{
		{Type: "click", Seq: 3, TS: base.Add(2 * time.Second), Data: json.RawMessage(`{"tag":"button","x":1.50}`)},
		{Type: "scroll", Seq: 1, TS: base.Add(time.Second)},
		{Type: "click", Seq: 4, TS: base.Add(time.Second), Data: json.RawMessage(`{"tag":"a"}`)},
		{Type: "navigation", Seq: 2, TS: base, Data: nil},
		// Two key events; seq 5 is the latest by ts.
		{Type: "sensitive_url", Seq: 5, TS: base.Add(4 * time.Second), Data: json.RawMessage(`{"category":"payment"}`)},
		{Type: "form_submit", Seq: 6, TS: base.Add(3 * time.Second)},
	}
	res, err := lc.RecordEvents(ctx, liveID, "sp", base, events)
	if err != nil || res.Inserted != len(events) {
		t.Fatalf("record events: inserted=%d err=%v", res.Inserted, err)
	}
}

// get performs GET path and asserts the no-store header every response
// must carry.
func (f *fixture) get(t *testing.T, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	f.h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("GET %s: Cache-Control = %q, want no-store", path, got)
	}
	return rec
}

func decodeInto(t *testing.T, rec *httptest.ResponseRecorder, dst any) {
	t.Helper()
	if err := json.Unmarshal(rec.Body.Bytes(), dst); err != nil {
		t.Fatalf("body %q: %v", rec.Body.String(), err)
	}
}

func errorOf(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var m map[string]string
	decodeInto(t, rec, &m)
	return m["error"]
}

func TestListSessions(t *testing.T) {
	f := newFixture(t)
	rec := f.get(t, "/sessions")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
	}
	var got listResponse
	decodeInto(t, rec, &got)
	if len(got.Sessions) != 2 {
		t.Fatalf("got %d sessions, want 2: %+v", len(got.Sessions), got.Sessions)
	}

	live, ended := got.Sessions[0], got.Sessions[1]
	if live.SessionID != liveID || live.Status != session.StatusLive {
		t.Errorf("first = %+v, want live %s", live, liveID)
	}
	if live.EndedAt != nil {
		t.Errorf("live ended_at = %v, want nil", live.EndedAt)
	}
	if !live.StartedAt.Equal(liveStart) || live.ParticipantCount != 2 || live.SpaceID != "sp" {
		t.Errorf("live = %+v", live)
	}
	if live.Source != store.SourceWebhook {
		t.Errorf("live source = %q, want %q", live.Source, store.SourceWebhook)
	}
	if string(live.Metadata) != `{"plan":"pro"}` {
		t.Errorf("live metadata = %s", live.Metadata)
	}
	if live.KeyEventCount != 2 {
		t.Errorf("live key_event_count = %d, want 2", live.KeyEventCount)
	}
	if lk := live.LastKeyEvent; lk == nil || lk.Seq != 5 || lk.Type != "sensitive_url" ||
		lk.TS != base.Add(4*time.Second).UnixMilli() || lk.ReceivedAt.IsZero() ||
		string(lk.Data) != `{"category":"payment"}` {
		t.Errorf("live last_key_event = %+v", lk)
	}
	if ended.KeyEventCount != 0 || ended.LastKeyEvent != nil {
		t.Errorf("ended key events = %d / %+v, want 0 / nil", ended.KeyEventCount, ended.LastKeyEvent)
	}

	if ended.SessionID != endedID || ended.Status != session.StatusEnded {
		t.Errorf("second = %+v, want ended %s", ended, endedID)
	}
	if ended.EndedAt == nil || !ended.EndedAt.Equal(endedAt) {
		t.Errorf("ended ended_at = %v, want %v", ended.EndedAt, endedAt)
	}
	if string(ended.Metadata) != `{}` {
		t.Errorf("ended metadata = %s, want {}", ended.Metadata)
	}

	// Raw shape: ended_at null for the live session, metadata an object.
	body := rec.Body.String()
	for _, want := range []string{`"ended_at":null`, `"metadata":{}`, `"source":"webhook"`,
		`"key_event_count":0`, `"last_key_event":null`, `"last_key_event":{"seq":5,`} {
		if !strings.Contains(body, want) {
			t.Errorf("body %s lacks %s", body, want)
		}
	}
}

func TestListSessionsLimit(t *testing.T) {
	f := newFixture(t)

	rec := f.get(t, "/sessions?limit=1")
	if rec.Code != http.StatusOK {
		t.Fatalf("limit=1 status = %d, body %s", rec.Code, rec.Body)
	}
	var got listResponse
	decodeInto(t, rec, &got)
	if len(got.Sessions) != 1 || got.Sessions[0].SessionID != liveID {
		t.Errorf("limit=1 sessions = %+v, want just %s", got.Sessions, liveID)
	}

	rec = f.get(t, "/sessions?limit=9999")
	if rec.Code != http.StatusOK {
		t.Errorf("limit=9999 status = %d, body %s", rec.Code, rec.Body)
	}
	decodeInto(t, rec, &got)
	if len(got.Sessions) != 2 {
		t.Errorf("limit=9999 got %d sessions, want 2", len(got.Sessions))
	}

	for _, bad := range []string{"0", "-1", "x", "1.5"} {
		rec := f.get(t, "/sessions?limit="+bad)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("limit=%s status = %d, want 400", bad, rec.Code)
			continue
		}
		if msg := errorOf(t, rec); msg != "limit: must be a positive integer" {
			t.Errorf("limit=%s error = %q", bad, msg)
		}
	}
}

func TestListSessionsEmpty(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	db, err := store.Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := store.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, "TRUNCATE events, sessions CASCADE"); err != nil {
		t.Fatal(err)
	}
	f := &fixture{h: Handler(session.New(store.New(db), stream.NewHub()))}

	rec := f.get(t, "/sessions")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
	}
	if body := strings.TrimSpace(rec.Body.String()); body != `{"sessions":[]}` {
		t.Errorf("body = %s, want {\"sessions\":[]}", body)
	}
}

func TestGetSession(t *testing.T) {
	f := newFixture(t)
	rec := f.get(t, "/sessions/"+liveID)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
	}
	var got Session
	decodeInto(t, rec, &got)
	if got.SessionID != liveID || got.SpaceID != "sp" || got.Status != session.StatusLive ||
		!got.StartedAt.Equal(liveStart) || got.EndedAt != nil || got.ParticipantCount != 2 ||
		got.Source != store.SourceWebhook {
		t.Errorf("session = %+v", got)
	}

	rec = f.get(t, "/sessions/"+endedID)
	if rec.Code != http.StatusOK {
		t.Fatalf("ended status = %d, body %s", rec.Code, rec.Body)
	}
	decodeInto(t, rec, &got)
	if got.SessionID != endedID || got.Status != session.StatusEnded || got.EndedAt == nil || !got.EndedAt.Equal(endedAt) {
		t.Errorf("ended session = %+v", got)
	}
}

func TestGetSessionNotFound(t *testing.T) {
	f := newFixture(t)
	rec := f.get(t, "/sessions/nope")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
	}
	if msg := errorOf(t, rec); msg != "not found" {
		t.Errorf("error = %q, want not found", msg)
	}
}

func TestListEvents(t *testing.T) {
	f := newFixture(t)
	rec := f.get(t, "/sessions/"+liveID+"/events")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
	}
	var got eventsResponse
	decodeInto(t, rec, &got)
	if got.SessionID != liveID {
		t.Errorf("session_id = %q, want %q", got.SessionID, liveID)
	}
	if len(got.Events) != len(wantSeq) {
		t.Fatalf("got %d events, want %d: %+v", len(got.Events), len(wantSeq), got.Events)
	}
	for i, e := range got.Events {
		if e.Seq != wantSeq[i] {
			t.Errorf("position %d: seq %d, want %d", i, e.Seq, wantSeq[i])
		}
		if e.ReceivedAt.IsZero() {
			t.Errorf("seq %d: received_at zero", e.Seq)
		}
	}
	bySeq := map[int]Event{}
	for _, e := range got.Events {
		bySeq[e.Seq] = e
	}
	if ts := bySeq[3].TS; ts != base.Add(2*time.Second).UnixMilli() {
		t.Errorf("seq 3 ts = %d, want %d", ts, base.Add(2*time.Second).UnixMilli())
	}
	if d := string(bySeq[2].Data); d != `{}` {
		t.Errorf("seq 2 (nil data) data = %s, want {}", d)
	}

	// Data comes back as stored: jsonb keeps the values (including the
	// number's own spelling) but orders keys its own way, so compare
	// decoded and check the literal.
	var data map[string]any
	if err := json.Unmarshal(bySeq[3].Data, &data); err != nil {
		t.Fatalf("seq 3 data %s: %v", bySeq[3].Data, err)
	}
	if data["tag"] != "button" || data["x"] != 1.5 || len(data) != 2 {
		t.Errorf("seq 3 data = %s", bySeq[3].Data)
	}

	// Raw body: ts is an integer, data keeps the number literal.
	body := rec.Body.String()
	if !regexp.MustCompile(`"ts":1767322447000[,}]`).MatchString(body) {
		t.Errorf("body %s: seq 3 ts not an epoch-ms integer", body)
	}
	if !strings.Contains(body, `"x":1.50`) {
		t.Errorf("body %s: seq 3 data not passed through", body)
	}
	if strings.Contains(body, `"received_at":0`) || !strings.Contains(body, `"received_at":"`) {
		t.Errorf("body %s: received_at not RFC 3339", body)
	}
}

func TestListEventsNone(t *testing.T) {
	f := newFixture(t)
	rec := f.get(t, "/sessions/"+endedID+"/events")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
	}
	if body := strings.TrimSpace(rec.Body.String()); body != `{"session_id":"`+endedID+`","events":[]}` {
		t.Errorf("body = %s, want empty events array", body)
	}
}

func TestListEventsNotFound(t *testing.T) {
	f := newFixture(t)
	rec := f.get(t, "/sessions/nope/events")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
	}
	if msg := errorOf(t, rec); msg != "not found" {
		t.Errorf("error = %q, want not found", msg)
	}
}
