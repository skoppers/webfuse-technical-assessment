package api

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/skoppers/webfuse-activity-analyzer/server/internal/session"
)

func marshal(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestToEvent(t *testing.T) {
	ts := time.UnixMilli(1767322445123).UTC()
	received := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	data := json.RawMessage(`{"tag":"button","x":1.50}`)

	got := toEvent(session.Event{
		ID: 9, SessionID: "s1", Seq: 7, Type: "click", TS: ts, ReceivedAt: received, Data: data,
	})
	if got.Seq != 7 || got.Type != "click" || got.TS != 1767322445123 || !got.ReceivedAt.Equal(received) {
		t.Errorf("toEvent = %+v", got)
	}
	if string(got.Data) != string(data) {
		t.Errorf("data = %s, want %s untouched", got.Data, data)
	}

	body := marshal(t, got)
	for _, want := range []string{`"ts":1767322445123`, `"data":{"tag":"button","x":1.50}`, `"received_at":"2026-01-02T03:04:05Z"`, `"seq":7`} {
		if !strings.Contains(body, want) {
			t.Errorf("json %s lacks %s", body, want)
		}
	}
	if strings.Contains(body, "session_id") || strings.Contains(body, `"id"`) {
		t.Errorf("json %s leaks row-only fields", body)
	}
}

func TestToEventEmptyData(t *testing.T) {
	for _, data := range []json.RawMessage{nil, {}} {
		body := marshal(t, toEvent(session.Event{Seq: 1, Type: "scroll", Data: data}))
		if !strings.Contains(body, `"data":{}`) {
			t.Errorf("data %#v: json %s, want data {}", data, body)
		}
	}
}

func TestToSession(t *testing.T) {
	started := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	ended := started.Add(time.Minute)
	meta := json.RawMessage(`{"plan":"pro"}`)

	got := toSession(session.Session{
		ID: "s1", SpaceID: "sp", Status: session.StatusEnded, StartedAt: started, EndedAt: &ended,
		Source: "webhook", ParticipantCount: 2, Metadata: meta, LastWebhookSeq: 5,
	})
	if got.SessionID != "s1" || got.SpaceID != "sp" || got.Status != "ended" || got.ParticipantCount != 2 || got.Source != "webhook" {
		t.Errorf("toSession = %+v", got)
	}
	if got.EndedAt == nil || !got.EndedAt.Equal(ended) {
		t.Errorf("ended_at = %v, want %v", got.EndedAt, ended)
	}
	body := marshal(t, got)
	for _, want := range []string{`"session_id":"s1"`, `"started_at":"2026-01-02T03:04:05Z"`, `"ended_at":"2026-01-02T03:05:05Z"`, `"metadata":{"plan":"pro"}`, `"source":"webhook"`} {
		if !strings.Contains(body, want) {
			t.Errorf("json %s lacks %s", body, want)
		}
	}
	if strings.Contains(body, "last_webhook_seq") {
		t.Errorf("json %s leaks row-only fields", body)
	}
}

func TestToSessionLiveWithoutMetadata(t *testing.T) {
	got := toSession(session.Session{ID: "s1", Status: session.StatusLive, Source: "event"})
	if got.EndedAt != nil {
		t.Errorf("ended_at = %v, want nil", got.EndedAt)
	}
	body := marshal(t, got)
	for _, want := range []string{`"ended_at":null`, `"metadata":{}`} {
		if !strings.Contains(body, want) {
			t.Errorf("json %s lacks %s", body, want)
		}
	}
}
