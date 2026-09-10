package stream

import (
	"testing"
	"time"
)

func recv(t *testing.T, ch <-chan Message) Message {
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
	return Message{}
}

func assertNone(t *testing.T, ch <-chan Message) {
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

func TestOverviewReceivesSessionAndKeyActivityOnly(t *testing.T) {
	h := NewHub()
	ch, cancel := h.Subscribe(Filter{})
	defer cancel()

	h.Publish(Message{Event: EventSession, SessionID: "a"})
	h.Publish(Message{Event: EventActivity, SessionID: "a", Key: false})
	h.Publish(Message{Event: EventActivity, SessionID: "b", Key: true})
	h.Publish(Message{Event: EventSession, SessionID: "b"})

	if m := recv(t, ch); m.Event != EventSession || m.SessionID != "a" {
		t.Errorf("first: %+v", m)
	}
	if m := recv(t, ch); m.Event != EventActivity || m.SessionID != "b" || !m.Key {
		t.Errorf("second: %+v", m)
	}
	if m := recv(t, ch); m.Event != EventSession || m.SessionID != "b" {
		t.Errorf("third: %+v", m)
	}
	assertNone(t, ch)
}

func TestPerSessionReceivesOnlyItsSession(t *testing.T) {
	h := NewHub()
	ch, cancel := h.Subscribe(Filter{SessionID: "a"})
	defer cancel()

	h.Publish(Message{Event: EventSession, SessionID: "b"})
	h.Publish(Message{Event: EventActivity, SessionID: "b", Key: true})
	h.Publish(Message{Event: EventActivity, SessionID: "a", Key: false})
	h.Publish(Message{Event: EventSession, SessionID: "a"})

	if m := recv(t, ch); m.Event != EventActivity || m.SessionID != "a" {
		t.Errorf("first: %+v", m)
	}
	if m := recv(t, ch); m.Event != EventSession || m.SessionID != "a" {
		t.Errorf("second: %+v", m)
	}
	assertNone(t, ch)
}

func TestCancelUnsubscribesAndClosesChannel(t *testing.T) {
	h := NewHub()
	ch, cancel := h.Subscribe(Filter{})
	if h.Len() != 1 {
		t.Fatalf("Len = %d, want 1", h.Len())
	}
	cancel()
	cancel() // idempotent
	if h.Len() != 0 {
		t.Fatalf("Len after cancel = %d, want 0", h.Len())
	}
	h.Publish(Message{Event: EventSession, SessionID: "a"})
	if _, ok := <-ch; ok {
		t.Fatal("received on cancelled subscription")
	}
}

func TestSlowSubscriberIsDroppedWithoutBlocking(t *testing.T) {
	h := NewHub()
	slow, cancelSlow := h.Subscribe(Filter{})
	defer cancelSlow()
	fast, cancelFast := h.Subscribe(Filter{})
	defer cancelFast()

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < subscriberBuffer+1; i++ {
			h.Publish(Message{Event: EventSession, SessionID: "a"})
			// Keep the fast subscriber drained so only the slow one overflows.
			<-fast
		}
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Publish blocked on a slow subscriber")
	}

	if h.Len() != 1 {
		t.Fatalf("Len = %d, want 1 (slow subscriber dropped)", h.Len())
	}
	// The buffered messages are still readable, then the channel is closed.
	for i := 0; i < subscriberBuffer; i++ {
		recv(t, slow)
	}
	if _, ok := <-slow; ok {
		t.Fatal("slow subscriber channel not closed")
	}
	// Publishing after the drop still reaches the remaining subscriber.
	h.Publish(Message{Event: EventSession, SessionID: "a"})
	recv(t, fast)
}

func TestIDsStrictlyIncrease(t *testing.T) {
	h := NewHub()
	ch, cancel := h.Subscribe(Filter{})
	defer cancel()

	h.Publish(Message{Event: EventSession, SessionID: "a"})
	h.Publish(Message{Event: EventActivity, SessionID: "a"}) // not delivered, still consumes an id
	h.Publish(Message{Event: EventSession, SessionID: "a"})

	var last uint64
	for i := 0; i < 2; i++ {
		m := recv(t, ch)
		if m.ID <= last {
			t.Fatalf("id %d not greater than previous %d", m.ID, last)
		}
		last = m.ID
	}
	if last != 3 {
		t.Errorf("last id = %d, want 3", last)
	}
}

func TestCloseEndsAllSubscribersAndRejectsNew(t *testing.T) {
	h := NewHub()
	overview, _ := h.Subscribe(Filter{})
	one, cancel := h.Subscribe(Filter{SessionID: "s1"})

	h.Close()

	for name, ch := range map[string]<-chan Message{"overview": overview, "s1": one} {
		if _, ok := <-ch; ok {
			t.Errorf("%s: channel still open after Close", name)
		}
	}
	if h.Len() != 0 {
		t.Errorf("Len after Close = %d, want 0", h.Len())
	}
	cancel() // must not panic after Close

	late, _ := h.Subscribe(Filter{})
	if _, ok := <-late; ok {
		t.Error("Subscribe after Close returned an open channel")
	}
	h.Publish(Message{Event: EventSession, SessionID: "s1"}) // no-op, must not panic
	h.Close()                                                // idempotent
}
