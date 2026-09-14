package ingest

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/skoppers/webfuse-activity-analyzer/server/internal/event"
)

var now = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

// valid returns a batch that passes Validate against now; tests mutate it.
func valid() Batch {
	return Batch{
		SessionID: "s1",
		SpaceID:   "sp",
		ClientID:  "c1",
		Events: []Event{
			{Type: "click", Seq: 1, TS: now.UnixMilli(), Data: json.RawMessage(`{"tag":"button"}`)},
			{Type: "form_submit", Seq: 2, TS: now.Add(time.Second).UnixMilli()},
		},
	}
}

func TestValidate(t *testing.T) {
	day := 24 * time.Hour
	tests := []struct {
		name    string
		mutate  func(b *Batch)
		wantErr string // substring of the error, "" for accepted
	}{
		{"valid", func(*Batch) {}, ""},
		{"empty session_id", func(b *Batch) { b.SessionID = "" }, "session_id"},
		{"session_id too long", func(b *Batch) { b.SessionID = strings.Repeat("x", 129) }, "session_id"},
		{"session_id at limit", func(b *Batch) { b.SessionID = strings.Repeat("x", 128) }, ""},
		{"empty space_id", func(b *Batch) { b.SpaceID = "" }, "space_id"},
		{"empty client_id", func(b *Batch) { b.ClientID = "" }, "client_id"},
		{"client_id too long", func(b *Batch) { b.ClientID = strings.Repeat("x", 65) }, "client_id"},
		{"client_id at limit", func(b *Batch) { b.ClientID = strings.Repeat("x", 64) }, ""},
		{"client_id multibyte at limit", func(b *Batch) { b.ClientID = strings.Repeat("é", 64) }, ""},
		{"no events", func(b *Batch) { b.Events = nil }, "events"},
		{"501 events", func(b *Batch) { b.Events = repeatEvents(501) }, "events"},
		{"500 events", func(b *Batch) { b.Events = repeatEvents(500) }, ""},
		{"unknown type", func(b *Batch) { b.Events[1].Type = "mousemove" }, "events[1].type"},
		{"seq 0", func(b *Batch) { b.Events[0].Seq = 0 }, "events[0].seq"},
		{"seq over int32", func(b *Batch) { b.Events[1].Seq = 1 << 31 }, "events[1].seq"},
		{"ts 2 days past", func(b *Batch) { b.Events[0].TS = now.Add(-2 * day).UnixMilli() }, "events[0].ts"},
		{"ts 2 days future", func(b *Batch) { b.Events[0].TS = now.Add(2 * day).UnixMilli() }, "events[0].ts"},
		{"ts 23 hours past", func(b *Batch) { b.Events[0].TS = now.Add(-23 * time.Hour).UnixMilli() }, ""},
		{"data too large", func(b *Batch) { b.Events[0].Data = bigObject(4097) }, "events[0].data"},
		{"data at limit", func(b *Batch) { b.Events[0].Data = bigObject(4096) }, ""},
		{"data array", func(b *Batch) { b.Events[0].Data = json.RawMessage(`[1]`) }, "events[0].data"},
		{"data string", func(b *Batch) { b.Events[0].Data = json.RawMessage(`"x"`) }, "events[0].data"},
		{"data number", func(b *Batch) { b.Events[0].Data = json.RawMessage(`1`) }, "events[0].data"},
		{"data malformed object", func(b *Batch) { b.Events[0].Data = json.RawMessage(`{"a":`) }, "events[0].data"},
		{"data null", func(b *Batch) { b.Events[0].Data = json.RawMessage(`null`) }, ""},
		{"data absent", func(b *Batch) { b.Events[0].Data = nil }, ""},
		{"data object with leading space", func(b *Batch) { b.Events[0].Data = json.RawMessage(` {"a":1}`) }, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			b := valid()
			tc.mutate(&b)
			err := Validate(b, now)
			switch {
			case tc.wantErr == "" && err != nil:
				t.Fatalf("Validate = %v, want nil", err)
			case tc.wantErr != "" && err == nil:
				t.Fatalf("Validate = nil, want error naming %q", tc.wantErr)
			case tc.wantErr != "" && !strings.Contains(err.Error(), tc.wantErr):
				t.Fatalf("Validate = %q, want it to name %q", err, tc.wantErr)
			}
		})
	}
}

func TestValidateErrorMessage(t *testing.T) {
	b := valid()
	b.Events[1].Seq = 0
	err := Validate(b, now)
	if want := "events[1].seq: must be between 1 and 2147483647"; err == nil || err.Error() != want {
		t.Fatalf("Validate = %v, want %q", err, want)
	}
}

func TestValidateDecodedJSON(t *testing.T) {
	// The same rules hold for a batch decoded off the wire, where a missing
	// data key is nil and an explicit null is the literal.
	var b Batch
	body := `{"session_id":"s1","space_id":"sp","client_id":"c1","events":[
		{"type":"click","seq":1,"ts":` + itoa(now.UnixMilli()) + `},
		{"type":"scroll","seq":2,"ts":` + itoa(now.UnixMilli()) + `,"data":null},
		{"type":"input","seq":3,"ts":` + itoa(now.UnixMilli()) + `,"data":{"len":3}}]}`
	if err := json.Unmarshal([]byte(body), &b); err != nil {
		t.Fatal(err)
	}
	if err := Validate(b, now); err != nil {
		t.Fatalf("Validate = %v, want nil", err)
	}
}

func TestToNewEvents(t *testing.T) {
	b := Batch{Events: []Event{
		{Type: "click", Seq: 7, TS: 1767322445123, Data: json.RawMessage(`{"a":1}`)},
		{Type: "scroll", Seq: 8, TS: 0, Data: json.RawMessage(`null`)},
		{Type: "input", Seq: 9, TS: -1000},
	}}
	got := toNewEvents(b)
	if len(got) != 3 {
		t.Fatalf("got %d events, want 3", len(got))
	}
	want0 := time.Date(2026, 1, 2, 2, 54, 5, 123_000_000, time.UTC)
	if got[0].Type != event.Click || got[0].Seq != 7 || !got[0].TS.Equal(want0) || string(got[0].Data) != `{"a":1}` {
		t.Errorf("event 0 = %+v, want ts %v", got[0], want0)
	}
	if got[1].Type != event.Scroll || !got[1].TS.Equal(time.Unix(0, 0)) || got[1].Data != nil {
		t.Errorf("event 1 = %+v, want epoch and nil data for null", got[1])
	}
	if got[2].Type != event.Input || !got[2].TS.Equal(time.Unix(-1, 0)) || got[2].Data != nil {
		t.Errorf("event 2 = %+v, want -1s and nil data", got[2])
	}
}

func repeatEvents(n int) []Event {
	out := make([]Event, n)
	for i := range out {
		out[i] = Event{Type: "click", Seq: i + 1, TS: now.UnixMilli()}
	}
	return out
}

// bigObject returns a JSON object of exactly size bytes.
func bigObject(size int) json.RawMessage {
	const frame = `{"k":""}`
	return json.RawMessage(`{"k":"` + strings.Repeat("x", size-len(frame)) + `"}`)
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }
