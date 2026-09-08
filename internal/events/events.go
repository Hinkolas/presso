// Package events is the in-process pub/sub hub behind the engine's
// server-sent event stream. Workers publish typed events (site status
// changes, job progress, platform health); the API fans them out to connected
// UI clients. Delivery is best effort: a subscriber that cannot keep up loses
// events rather than stalling publishers, and the UI re-fetches state on
// reconnect.
package events

import (
	"sync"
	"time"
)

// Event types. Payload shapes are documented on the publishing package.
const (
	TypeSiteStatus     = "site.status"
	TypeSiteDeleted    = "site.deleted"
	TypeJobUpdated     = "job.updated"
	TypeJobEvent       = "job.event"
	TypePlatformStatus = "platform.status"
	// TypeSystemUpdate carries the update check result (internal/update).
	TypeSystemUpdate = "system.update"
)

type Event struct {
	Type    string    `json:"type"`
	Time    time.Time `json:"time"`
	Payload any       `json:"payload"`
}

type Hub struct {
	mu   sync.RWMutex
	subs map[chan Event]struct{}
}

func New() *Hub {
	return &Hub{subs: make(map[chan Event]struct{})}
}

// Publish delivers to every subscriber without blocking; full subscribers
// drop the event.
func (h *Hub) Publish(typ string, payload any) {
	ev := Event{Type: typ, Time: time.Now().UTC(), Payload: payload}
	h.mu.RLock()
	defer h.mu.RUnlock()
	for ch := range h.subs {
		select {
		case ch <- ev:
		default:
		}
	}
}

// Subscribe returns a channel of events and a cancel function that must be
// called to release the subscription.
func (h *Hub) Subscribe(buffer int) (<-chan Event, func()) {
	if buffer <= 0 {
		buffer = 64
	}
	ch := make(chan Event, buffer)
	h.mu.Lock()
	h.subs[ch] = struct{}{}
	h.mu.Unlock()
	var once sync.Once
	cancel := func() {
		once.Do(func() {
			h.mu.Lock()
			delete(h.subs, ch)
			h.mu.Unlock()
		})
	}
	return ch, cancel
}

// Subscribers reports the live subscription count (diagnostics).
func (h *Hub) Subscribers() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.subs)
}
