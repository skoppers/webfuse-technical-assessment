package webhook

import (
	"strings"
	"testing"
	"time"
)

const (
	payloadStarted = `{"category":"space.session.started","created_at":"2025-02-07T17:06:51.197164+00:00","data":{"duration":null,"first_member":null,"link":"https://webfuse.com/sZQM9askvSCqSa2I2cJfQJujA","participant_count":0,"pin":null,"queue_duration":null,"session_id":"sZQM9askvSCqSa2I2cJfQJujA","space_id":23,"started_at":"2025-02-07T17:06:51.184169+00:00"},"sequence_id":40}`
	payloadEnded   = `{"category":"space.session.ended","created_at":"2025-02-07T17:16:51.197164+00:00","data":{"duration":600,"first_member":null,"link":"https://webfuse.com/sZQM9askvSCqSa2I2cJfQJujA","participant_count":2,"pin":null,"queue_duration":null,"session_id":"sZQM9askvSCqSa2I2cJfQJujA","space_id":23,"started_at":"2025-02-07T17:06:51.184169+00:00"},"sequence_id":53}`
	payloadJoined  = `{"category":"space.session.participant_joined","created_at":"2025-02-07T17:08:00.000000+00:00","data":{"session_id":"sZQM9askvSCqSa2I2cJfQJujA","space_id":23,"space_name":"My Space","participant_count":2},"sequence_id":56}`
	payloadLeft    = `{"category":"space.session.participant_left","created_at":"2025-02-07T17:12:00.000000+00:00","data":{"session_id":"sZQM9askvSCqSa2I2cJfQJujA","space_id":23,"space_name":"My Space","email":"alice@example.com","name":"Alice","client_index":1,"user_id":42,"is_leader":false},"sequence_id":57}`

	sessionID = "sZQM9askvSCqSa2I2cJfQJujA"
)

func mustParse(t *testing.T, s string) Envelope {
	t.Helper()
	env, err := Parse([]byte(s))
	if err != nil {
		t.Fatal(err)
	}
	return env
}

func TestParseStarted(t *testing.T) {
	env := mustParse(t, payloadStarted)
	if env.Category != CategorySessionStarted || env.SequenceID != 40 {
		t.Fatalf("got %+v", env)
	}
	want := time.Date(2025, 2, 7, 17, 6, 51, 197164000, time.UTC)
	if !env.CreatedAt.Equal(want) {
		t.Fatalf("created_at %v, want %v", env.CreatedAt, want)
	}
	d, err := env.SessionData()
	if err != nil {
		t.Fatal(err)
	}
	if d.SessionID != sessionID || d.SpaceID != "23" || d.ParticipantCount != 0 || d.Duration != nil {
		t.Fatalf("got %+v", d)
	}
	if !d.StartedAt.Equal(time.Date(2025, 2, 7, 17, 6, 51, 184169000, time.UTC)) {
		t.Fatalf("started_at %v", d.StartedAt)
	}
}

func TestParseEnded(t *testing.T) {
	env := mustParse(t, payloadEnded)
	if env.Category != CategorySessionEnded || env.SequenceID != 53 {
		t.Fatalf("got %+v", env)
	}
	if !env.CreatedAt.Equal(time.Date(2025, 2, 7, 17, 16, 51, 197164000, time.UTC)) {
		t.Fatalf("created_at %v", env.CreatedAt)
	}
	d, err := env.SessionData()
	if err != nil {
		t.Fatal(err)
	}
	if d.SessionID != sessionID || d.SpaceID != "23" || d.ParticipantCount != 2 {
		t.Fatalf("got %+v", d)
	}
	if d.Duration == nil || *d.Duration != 600 {
		t.Fatalf("duration %v", d.Duration)
	}
}

func TestParseJoined(t *testing.T) {
	env := mustParse(t, payloadJoined)
	if env.Category != CategoryParticipantJoined || env.SequenceID != 56 {
		t.Fatalf("got %+v", env)
	}
	if !env.CreatedAt.Equal(time.Date(2025, 2, 7, 17, 8, 0, 0, time.UTC)) {
		t.Fatalf("created_at %v", env.CreatedAt)
	}
	d, err := env.ParticipantData()
	if err != nil {
		t.Fatal(err)
	}
	want := ParticipantData{SessionID: sessionID, SpaceID: "23", SpaceName: "My Space", ParticipantCount: 2}
	if d != want {
		t.Fatalf("got %+v, want %+v", d, want)
	}
}

func TestParseLeft(t *testing.T) {
	env := mustParse(t, payloadLeft)
	if env.Category != CategoryParticipantLeft || env.SequenceID != 57 {
		t.Fatalf("got %+v", env)
	}
	if !env.CreatedAt.Equal(time.Date(2025, 2, 7, 17, 12, 0, 0, time.UTC)) {
		t.Fatalf("created_at %v", env.CreatedAt)
	}
	d, err := env.ParticipantData()
	if err != nil {
		t.Fatal(err)
	}
	want := ParticipantData{SessionID: sessionID, SpaceID: "23", SpaceName: "My Space", ClientIndex: 1}
	if d != want {
		t.Fatalf("got %+v, want %+v", d, want)
	}
}

func TestSpaceIDForms(t *testing.T) {
	env := mustParse(t, strings.Replace(payloadJoined, `"space_id":23`, `"space_id":"23"`, 1))
	d, err := env.ParticipantData()
	if err != nil {
		t.Fatal(err)
	}
	if d.SpaceID != "23" {
		t.Fatalf("space_id %q", d.SpaceID)
	}
	env = mustParse(t, strings.Replace(payloadJoined, `"space_id":23`, `"space_id":true`, 1))
	if _, err := env.ParticipantData(); err == nil {
		t.Fatal("want error for boolean space_id")
	}
	env = mustParse(t, strings.Replace(payloadStarted, `"space_id":23`, `"space_id":[23]`, 1))
	if _, err := env.SessionData(); err == nil {
		t.Fatal("want error for array space_id")
	}
}

func TestParseRejects(t *testing.T) {
	cases := map[string]string{
		"missing category": strings.Replace(payloadStarted, `"category":"space.session.started",`, "", 1),
		"empty category":   strings.Replace(payloadStarted, `"space.session.started"`, `""`, 1),
		"sequence_id 0":    strings.Replace(payloadStarted, `"sequence_id":40`, `"sequence_id":0`, 1),
		"sequence_id -1":   strings.Replace(payloadStarted, `"sequence_id":40`, `"sequence_id":-1`, 1),
		"no sequence_id":   strings.Replace(payloadStarted, `,"sequence_id":40`, ``, 1),
		"bad created_at":   strings.Replace(payloadStarted, `2025-02-07T17:06:51.197164+00:00`, `yesterday`, 1),
		"no created_at":    strings.Replace(payloadStarted, `"created_at":"2025-02-07T17:06:51.197164+00:00",`, ``, 1),
		"not json":         `{`,
	}
	for name, body := range cases {
		if env, err := Parse([]byte(body)); err == nil {
			t.Errorf("%s: parsed as %+v", name, env)
		}
	}
}

func TestSessionDataIgnoresCategory(t *testing.T) {
	env := mustParse(t, payloadJoined)
	d, err := env.SessionData()
	if err != nil {
		t.Fatal(err)
	}
	if d.SessionID != sessionID || d.SpaceID != "23" || d.ParticipantCount != 2 {
		t.Fatalf("got %+v", d)
	}
}
