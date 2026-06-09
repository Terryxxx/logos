// Package events is a tiny in-process pub/sub used to fan domain events
// (task:queued, issue:updated, …) out to the realtime hub and any other
// listeners we add later (analytics, notifications, …).
//
// Synchronous on purpose: keeps ordering deterministic and removes a class
// of "event arrived before the row committed" races. Listeners that do
// heavy work should spawn their own goroutine.
package events

import "sync"

type Event struct {
	Type    string
	Payload any
}

type Listener func(Event)

type Bus struct {
	mu        sync.RWMutex
	listeners []Listener
}

func NewBus() *Bus { return &Bus{} }

func (b *Bus) Subscribe(fn Listener) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.listeners = append(b.listeners, fn)
}

func (b *Bus) Publish(t string, payload any) {
	b.mu.RLock()
	ls := append([]Listener(nil), b.listeners...)
	b.mu.RUnlock()
	for _, fn := range ls {
		fn(Event{Type: t, Payload: payload})
	}
}
