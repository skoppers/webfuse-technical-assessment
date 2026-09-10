// Package stream fans session lifecycle and activity messages out from the
// parts of the server that produce them to the SSE clients watching them.
package stream

import "sync"

// Message event kinds. EventSession carries a SessionPayload; EventActivity
// carries an ActivityPayload.
const (
	EventSession  = "session"
	EventActivity = "activity"
)

// subscriberBuffer is the number of undelivered messages a subscriber may
// accumulate before the hub drops it.
const subscriberBuffer = 64

// Message is one item published through the hub. ID is assigned by Publish
// and strictly increases for the life of the hub; it is sent as the SSE id.
// Key marks an activity message that overview subscribers should also see.
type Message struct {
	ID        uint64
	Event     string
	SessionID string
	Key       bool
	Data      any
}

// Filter selects which messages a subscriber receives. An empty SessionID is
// the overview: every session message plus key activity messages. A
// non-empty SessionID receives every message for that session and nothing
// else.
type Filter struct {
	SessionID string
}

func (f Filter) matches(m Message) bool {
	if f.SessionID != "" {
		return m.SessionID == f.SessionID
	}
	return m.Event == EventSession || (m.Event == EventActivity && m.Key)
}

type subscriber struct {
	filter Filter
	ch     chan Message
}

// Hub is an in-process publish/subscribe fan-out. Publish never blocks: each
// subscriber has a bounded buffer and one whose buffer is full when a
// matching message arrives is dropped, meaning it is removed from the hub and
// its channel is closed. A dropped client is expected to reconnect and
// re-read current state from the read API. There is no replay.
//
// The zero value is not usable; use NewHub.
type Hub struct {
	mu     sync.Mutex
	nextID uint64
	subs   map[*subscriber]struct{}
	closed bool
}

// NewHub returns an empty hub.
func NewHub() *Hub {
	return &Hub{subs: make(map[*subscriber]struct{})}
}

// Publish assigns msg the next ID and delivers it to every subscriber whose
// filter matches, dropping any subscriber that cannot accept it immediately.
func (h *Hub) Publish(msg Message) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.nextID++
	msg.ID = h.nextID
	for s := range h.subs {
		if !s.filter.matches(msg) {
			continue
		}
		select {
		case s.ch <- msg:
		default:
			h.remove(s)
		}
	}
}

// Subscribe registers a subscriber and returns its channel and a cancel
// function. The channel is closed when the subscriber is cancelled or
// dropped. On a closed hub the returned channel is already closed.
// cancel is safe to call more than once.
func (h *Hub) Subscribe(filter Filter) (<-chan Message, func()) {
	s := &subscriber{filter: filter, ch: make(chan Message, subscriberBuffer)}
	h.mu.Lock()
	if h.closed {
		close(s.ch)
	} else {
		h.subs[s] = struct{}{}
	}
	h.mu.Unlock()
	cancel := func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		if _, ok := h.subs[s]; ok {
			h.remove(s)
		}
	}
	return s.ch, cancel
}

// Close drops every subscriber and rejects new ones, ending all open
// streams. It is used at server shutdown and is safe to call more than once.
func (h *Hub) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.closed = true
	for s := range h.subs {
		h.remove(s)
	}
}

// Len returns the number of current subscribers.
func (h *Hub) Len() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.subs)
}

// remove deletes s and closes its channel. Callers hold h.mu and have
// checked that s is present, so the channel is closed exactly once.
func (h *Hub) remove(s *subscriber) {
	delete(h.subs, s)
	close(s.ch)
}
