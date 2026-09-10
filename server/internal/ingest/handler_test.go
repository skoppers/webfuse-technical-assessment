package ingest

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/skoppers/webfuse-activity-analyzer/server/internal/session"
	"github.com/skoppers/webfuse-activity-analyzer/server/internal/store"
	"github.com/skoppers/webfuse-activity-analyzer/server/internal/stream"
)

const testOrigin = "http://extension.test"

// fixture is the ingest handler over a Lifecycle on a clean test database,
// with one per-session subscription on the hub.
type fixture struct {
	h   http.Handler
	one <-chan stream.Message
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
	one, cancel := hub.Subscribe(stream.Filter{SessionID: sessionID})
	t.Cleanup(cancel)
	return &fixture{h: Handler(session.New(store.New(db), hub), testOrigin), one: one}
}

// post sends body to POST / with an Origin header and returns the recorder.
func (f *fixture) post(body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", testOrigin)
	rec := httptest.NewRecorder()
	f.h.ServeHTTP(rec, req)
	return rec
}

func decode(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatalf("body %q: %v", rec.Body.String(), err)
	}
	return m
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

func batchJSON(t *testing.T, b Batch) string {
	t.Helper()
	body, err := json.Marshal(b)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func TestPostStoresPublishesAndDedups(t *testing.T) {
	f := newFixture(t, "s1")
	base := time.Now().Add(-time.Minute).UnixMilli()
	body := batchJSON(t, Batch{
		SessionID: "s1", SpaceID: "sp",
		Events: []Event{
			{Type: "click", Seq: 1, TS: base, Data: json.RawMessage(`{"tag":"button"}`)},
			{Type: "form_submit", Seq: 2, TS: base + 1000, Data: json.RawMessage(`{"fieldCount":3}`)},
			{Type: "scroll", Seq: 3, TS: base + 2000},
		},
	})

	rec := f.post(body)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
	}
	if got := decode(t, rec); got["accepted"] != 3.0 || got["duplicates"] != 0.0 {
		t.Errorf("body = %v, want accepted 3 duplicates 0", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != testOrigin {
		t.Errorf("Access-Control-Allow-Origin = %q, want %q", got, testOrigin)
	}

	// One session message for the new stub, then every event in batch order.
	if m := recv(t, f.one); m.Event != stream.EventSession || m.SessionID != "s1" {
		t.Errorf("first message = %+v, want session s1", m)
	}
	wantType := []string{"click", "form_submit", "scroll"}
	wantKey := []bool{false, true, false}
	for i := range wantType {
		m := recv(t, f.one)
		p, ok := m.Data.(stream.ActivityPayload)
		if m.Event != stream.EventActivity || !ok {
			t.Fatalf("message %d = %+v, want activity", i, m)
		}
		if p.Type != wantType[i] || p.Seq != i+1 || m.Key != wantKey[i] {
			t.Errorf("activity %d: key=%v payload=%+v", i, m.Key, p)
		}
		if p.Type == "form_submit" && string(p.Data) != `{"fieldCount":3}` {
			t.Errorf("form_submit data = %s", p.Data)
		}
	}
	assertNone(t, f.one)

	// Resend: nothing new stored, nothing published.
	rec = f.post(body)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("resend status = %d, body %s", rec.Code, rec.Body)
	}
	if got := decode(t, rec); got["accepted"] != 0.0 || got["duplicates"] != 3.0 {
		t.Errorf("resend body = %v, want accepted 0 duplicates 3", got)
	}
	assertNone(t, f.one)
}

func TestPostRejectsBadRequests(t *testing.T) {
	f := newFixture(t, "s1")
	tests := []struct {
		name    string
		body    string
		wantErr string
	}{
		{"invalid JSON", `{"session_id":`, "invalid JSON"},
		{"empty body", ``, "empty body"},
		{"missing space_id", batchJSON(t, Batch{SessionID: "s1", Events: []Event{{Type: "click", Seq: 1, TS: time.Now().UnixMilli()}}}), "space_id"},
		{"bad seq", batchJSON(t, Batch{SessionID: "s1", SpaceID: "sp", Events: []Event{{Type: "click", Seq: 0, TS: time.Now().UnixMilli()}}}), "events[0].seq"},
		{"unknown type", batchJSON(t, Batch{SessionID: "s1", SpaceID: "sp", Events: []Event{{Type: "mousemove", Seq: 1, TS: time.Now().UnixMilli()}}}), "events[0].type"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := f.post(tc.body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
			}
			msg, _ := decode(t, rec)["error"].(string)
			if !strings.Contains(msg, tc.wantErr) {
				t.Errorf("error = %q, want it to name %q", msg, tc.wantErr)
			}
			if got := rec.Header().Get("Access-Control-Allow-Origin"); got != testOrigin {
				t.Errorf("Access-Control-Allow-Origin = %q, want %q", got, testOrigin)
			}
		})
	}
	assertNone(t, f.one) // nothing stored, nothing published
}

func TestPreflight(t *testing.T) {
	f := newFixture(t, "s1")
	req := httptest.NewRequest(http.MethodOptions, "/", nil)
	req.Header.Set("Origin", testOrigin)
	req.Header.Set("Access-Control-Request-Method", "POST")
	rec := httptest.NewRecorder()
	f.h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
	want := map[string]string{
		"Access-Control-Allow-Origin":  testOrigin,
		"Access-Control-Allow-Methods": "POST",
		"Access-Control-Allow-Headers": "content-type",
		"Access-Control-Max-Age":       "600",
	}
	for k, v := range want {
		if got := rec.Header().Get(k); got != v {
			t.Errorf("%s = %q, want %q", k, got, v)
		}
	}
	if rec.Body.Len() != 0 {
		t.Errorf("preflight body = %q, want empty", rec.Body)
	}
}
