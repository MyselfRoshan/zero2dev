package events

import (
	"strings"
	"testing"
	"time"
)

func TestTicketSingleUse(t *testing.T) {
	h := New()
	tk := h.NewTicket()
	if !h.RedeemTicket(tk) {
		t.Fatal("first redeem should succeed")
	}
	if h.RedeemTicket(tk) {
		t.Fatal("second redeem must fail")
	}
	if h.RedeemTicket("nope") {
		t.Fatal("unknown ticket must fail")
	}
}

func TestTicketExpiry(t *testing.T) {
	h := New()
	now := time.Now()
	h.SetClock(func() time.Time { return now })
	tk := h.NewTicket()
	now = now.Add(TicketTTL + time.Second)
	if h.RedeemTicket(tk) {
		t.Fatal("expired ticket must fail")
	}
	tk2 := h.NewTicket()
	now = now.Add(TicketTTL - time.Second)
	if !h.RedeemTicket(tk2) {
		t.Fatal("ticket just inside TTL must work")
	}
}

func TestPublishFrameAndSlowDrop(t *testing.T) {
	h := New()
	fast := h.Subscribe()
	slow := h.Subscribe()
	h.Publish("x.y", map[string]int{"a": 1})
	got := string(<-fast.C)
	if got != "event: x.y\ndata: {\"a\":1}\n\n" {
		t.Fatalf("bad frame %q", got)
	}
	// fill slow's buffer (it already holds 1 frame) without draining
	for i := 0; i < subBuffer+5; i++ {
		h.Publish("n", i)
		select { // keep fast drained
		case <-fast.C:
		default:
		}
	}
	// slow must have been dropped: channel closed after draining buffer
	n := 0
	for range slow.C {
		n++
	}
	if n == 0 || n > subBuffer {
		t.Fatalf("unexpected buffered count %d", n)
	}
	if h.SubscriberCount() != 1 {
		t.Fatalf("want 1 subscriber, got %d", h.SubscriberCount())
	}
	h.Close()
	if _, ok := <-fast.C; ok {
		if _, ok := <-fast.C; ok {
			t.Fatal("fast should be closed after Close")
		}
	}
	if h.Subscribe() != nil {
		t.Fatal("subscribe after close must be nil")
	}
	_ = strings.TrimSpace
}
