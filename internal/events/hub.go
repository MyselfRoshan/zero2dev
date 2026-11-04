// Package events fans server-sent events out to subscribers and issues the
// short-lived single-use tickets that authorise the SSE connection.
package events

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"sync"
	"time"
)

const (
	// TicketTTL is how long a ticket stays valid.
	TicketTTL  = 30 * time.Second
	subBuffer  = 256
	maxTickets = 1000
)

// Sub is one SSE subscriber. C delivers pre-formatted frames and is closed
// when the hub drops the subscriber (slow consumer) or shuts down.
type Sub struct{ C chan []byte }

// Hub is the event bus.
type Hub struct {
	mu      sync.Mutex
	subs    map[*Sub]struct{}
	tickets map[string]time.Time
	closed  bool
	now     func() time.Time // overridable in tests
}

// New creates a Hub.
func New() *Hub {
	return &Hub{subs: map[*Sub]struct{}{}, tickets: map[string]time.Time{}, now: time.Now}
}

// SetClock replaces the time source (tests).
func (h *Hub) SetClock(f func() time.Time) { h.mu.Lock(); h.now = f; h.mu.Unlock() }

// Subscribe registers a subscriber. Returns nil if the hub is closed.
func (h *Hub) Subscribe() *Sub {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return nil
	}
	s := &Sub{C: make(chan []byte, subBuffer)}
	h.subs[s] = struct{}{}
	return s
}

// Unsubscribe removes s (safe to call after a drop).
func (h *Hub) Unsubscribe(s *Sub) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.subs[s]; ok {
		delete(h.subs, s)
		close(s.C)
	}
}

// SubscriberCount is used by tests.
func (h *Hub) SubscriberCount() int { h.mu.Lock(); defer h.mu.Unlock(); return len(h.subs) }

// Publish broadcasts an event. It never blocks: a subscriber whose buffer is
// full is dropped (its channel is closed and the client reconnects).
func (h *Hub) Publish(typ string, data any) {
	payload, err := json.Marshal(data)
	if err != nil {
		return
	}
	frame := []byte("event: " + typ + "\ndata: " + string(payload) + "\n\n")
	h.mu.Lock()
	defer h.mu.Unlock()
	for s := range h.subs {
		select {
		case s.C <- frame:
		default:
			delete(h.subs, s)
			close(s.C)
		}
	}
}

// Close drops every subscriber and refuses new ones.
func (h *Hub) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.closed = true
	for s := range h.subs {
		delete(h.subs, s)
		close(s.C)
	}
}

// NewTicket issues a random single-use ticket valid for TicketTTL.
func (h *Hub) NewTicket() string {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	t := hex.EncodeToString(b)
	h.mu.Lock()
	defer h.mu.Unlock()
	now := h.now()
	for k, exp := range h.tickets { // purge expired
		if now.After(exp) {
			delete(h.tickets, k)
		}
	}
	if len(h.tickets) >= maxTickets { // refuse to grow unbounded: drop oldest-ish
		for k := range h.tickets {
			delete(h.tickets, k)
			break
		}
	}
	h.tickets[t] = now.Add(TicketTTL)
	return t
}

// RedeemTicket consumes t. It returns true at most once per ticket and only
// before expiry.
func (h *Hub) RedeemTicket(t string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	exp, ok := h.tickets[t]
	if !ok {
		return false
	}
	delete(h.tickets, t) // single use, even if expired
	return !h.now().After(exp)
}
