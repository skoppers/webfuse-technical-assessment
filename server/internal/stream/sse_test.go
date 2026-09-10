package stream

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// streamServer serves the SSE handler and reports, via done, when each
// request's handler has returned.
type streamServer struct {
	*httptest.Server
	hub  *Hub
	done chan struct{}
}

func newStreamServer(t *testing.T, ping time.Duration) *streamServer {
	t.Helper()
	hub := NewHub()
	done := make(chan struct{}, 8)
	h := newHandler(hub, ping)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.ServeHTTP(w, r)
		done <- struct{}{}
	}))
	t.Cleanup(srv.Close)
	return &streamServer{Server: srv, hub: hub, done: done}
}

// open starts a stream request and returns the response and a cancel for it.
func (s *streamServer) open(t *testing.T, path string, hdr http.Header) (*http.Response, context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.URL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range hdr {
		req.Header[k] = v
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	t.Cleanup(func() { cancel(); resp.Body.Close() })
	return resp, cancel
}

// readFrame returns the next frame (lines up to and including the blank
// terminator) from the stream, skipping comment frames.
func readFrame(t *testing.T, sc *bufio.Scanner) string {
	t.Helper()
	got := make(chan string, 1)
	go func() {
		var lines []string
		for sc.Scan() {
			line := sc.Text()
			if line != "" {
				lines = append(lines, line)
				continue
			}
			if len(lines) == 1 && strings.HasPrefix(lines[0], ":") {
				lines = nil
				continue
			}
			got <- strings.Join(lines, "\n") + "\n\n"
			return
		}
		got <- ""
	}()
	select {
	case f := <-got:
		if f == "" {
			t.Fatal("stream ended before a frame arrived")
		}
		return f
	case <-time.After(2 * time.Second):
		t.Fatal("no frame within 2s")
	}
	return ""
}

func (s *streamServer) waitDone(t *testing.T) {
	t.Helper()
	select {
	case <-s.done:
	case <-time.After(2 * time.Second):
		t.Fatal("handler did not return")
	}
}

func TestSSEHeadersFrameAndCancel(t *testing.T) {
	s := newStreamServer(t, time.Hour)
	resp, cancel := s.open(t, "/", nil)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	for k, want := range map[string]string{
		"Content-Type":      "text/event-stream",
		"Cache-Control":     "no-cache",
		"Connection":        "keep-alive",
		"X-Accel-Buffering": "no",
	} {
		if got := resp.Header.Get(k); got != want {
			t.Errorf("%s = %q, want %q", k, got, want)
		}
	}
	if s.hub.Len() != 1 {
		t.Fatalf("Len = %d, want 1", s.hub.Len())
	}

	started := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	s.hub.Publish(Message{Event: EventSession, SessionID: "s1", Data: SessionPayload{
		SessionID: "s1", SpaceID: "sp", Status: "live", StartedAt: started, ParticipantCount: 2,
	}})

	sc := bufio.NewScanner(resp.Body)
	want := "id: 1\nevent: session\ndata: " +
		`{"session_id":"s1","space_id":"sp","status":"live","started_at":"2026-01-02T03:04:05Z","ended_at":null,"participant_count":2}` +
		"\n\n"
	if got := readFrame(t, sc); got != want {
		t.Errorf("frame:\n got %q\nwant %q", got, want)
	}

	cancel()
	s.waitDone(t)
	if s.hub.Len() != 0 {
		t.Errorf("Len after cancel = %d, want 0", s.hub.Len())
	}
}

func TestSSEOverviewFiltersActivity(t *testing.T) {
	s := newStreamServer(t, time.Hour)
	resp, _ := s.open(t, "/", nil)
	sc := bufio.NewScanner(resp.Body)

	ts := time.Date(2026, 1, 2, 3, 4, 5, 456000000, time.UTC)
	s.hub.Publish(Message{Event: EventActivity, SessionID: "s1", Key: false, Data: ActivityPayload{
		SessionID: "s1", Type: "click", Seq: 1, TS: ts.UnixMilli(), Data: json.RawMessage(`{"x":1}`),
	}})
	s.hub.Publish(Message{Event: EventActivity, SessionID: "s1", Key: true, Data: ActivityPayload{
		SessionID: "s1", Type: "form_submit", Seq: 2, TS: ts.UnixMilli(), Data: json.RawMessage(`{"fieldCount":3}`),
	}})

	want := "id: 2\nevent: activity\ndata: " +
		`{"session_id":"s1","type":"form_submit","seq":2,"ts":1767323045456,"data":{"fieldCount":3}}` +
		"\n\n"
	if got := readFrame(t, sc); got != want {
		t.Errorf("frame:\n got %q\nwant %q", got, want)
	}
}

func TestSSEPerSessionRoute(t *testing.T) {
	s := newStreamServer(t, time.Hour)
	resp, _ := s.open(t, "/s2", http.Header{"Last-Event-ID": {"7"}})
	sc := bufio.NewScanner(resp.Body)

	for _, id := range []string{"s1", "s2"} {
		s.hub.Publish(Message{Event: EventActivity, SessionID: id, Data: ActivityPayload{
			SessionID: id, Type: "click", Seq: 1, TS: 1, Data: json.RawMessage(`{}`),
		}})
	}

	want := "id: 2\nevent: activity\ndata: " +
		`{"session_id":"s2","type":"click","seq":1,"ts":1,"data":{}}` +
		"\n\n"
	if got := readFrame(t, sc); got != want {
		t.Errorf("frame:\n got %q\nwant %q", got, want)
	}
}

func TestSSEPing(t *testing.T) {
	s := newStreamServer(t, 20*time.Millisecond)
	resp, _ := s.open(t, "/", nil)
	sc := bufio.NewScanner(resp.Body)

	for i := 0; i < 2; i++ {
		if !sc.Scan() {
			t.Fatal("stream ended")
		}
		if got := sc.Text(); got != ": ping" {
			t.Fatalf("line %d = %q, want %q", i, got, ": ping")
		}
		if !sc.Scan() || sc.Text() != "" {
			t.Fatal("ping not followed by a blank line")
		}
	}
}

func TestSSEEndsWhenSubscriberDropped(t *testing.T) {
	s := newStreamServer(t, time.Hour)
	resp, _ := s.open(t, "/", nil)

	// Publish without reading until the handler's socket write blocks, its
	// buffer overflows and the hub drops it; the handler then returns.
	deadline := time.Now().Add(5 * time.Second)
	for s.hub.Len() != 0 {
		if time.Now().After(deadline) {
			t.Fatal("subscriber was never dropped")
		}
		s.hub.Publish(Message{Event: EventSession, SessionID: "s1", Data: strings.Repeat("x", 1024)})
	}
	s.waitDone(t)
	if s.hub.Len() != 0 {
		t.Errorf("Len = %d, want 0", s.hub.Len())
	}
	_ = resp
}

// noFlush hides the Flush method of the wrapped ResponseWriter.
type noFlush struct{ http.ResponseWriter }

func TestSSERequiresFlusher(t *testing.T) {
	h := newHandler(NewHub(), time.Hour)
	rec := httptest.NewRecorder()
	h.ServeHTTP(noFlush{rec}, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
}
