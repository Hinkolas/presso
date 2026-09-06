package events

import "testing"

func TestPublishSubscribe(t *testing.T) {
	h := New()
	ch, cancel := h.Subscribe(2)
	h.Publish("a", 1)
	h.Publish("b", 2)
	h.Publish("c", 3) // dropped: buffer full, publisher must not block
	if got := (<-ch).Type; got != "a" {
		t.Fatalf("got %s", got)
	}
	if got := (<-ch).Type; got != "b" {
		t.Fatalf("got %s", got)
	}
	select {
	case ev := <-ch:
		t.Fatalf("unexpected %v", ev)
	default:
	}
	cancel()
	cancel() // idempotent
	if h.Subscribers() != 0 {
		t.Fatal("subscriber not removed")
	}
	h.Publish("d", 4) // no subscribers, no panic
}
