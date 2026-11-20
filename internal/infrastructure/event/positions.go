package event

import (
	"sync"

	"cityflow/internal/domain/location"
)

// Positions is an in-process event bus. Multi-instance delivery needs a shared broker.
type Positions struct {
	mu        sync.Mutex
	listeners map[string]map[chan location.Position]struct{}
}

func NewPositions() *Positions {
	return &Positions{listeners: make(map[string]map[chan location.Position]struct{})}
}
func (b *Positions) Publish(p location.Position) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for ch := range b.listeners[p.RiderID] {
		// Keep the latest value: a slow browser must not block position reporting.
		select {
		case ch <- p:
		default:
			select {
			case <-ch:
			default:
			}
			ch <- p
		}
	}
}
func (b *Positions) Subscribe(id string) (<-chan location.Position, func()) {
	b.mu.Lock()
	defer b.mu.Unlock()
	ch := make(chan location.Position, 1)
	if b.listeners[id] == nil {
		b.listeners[id] = make(map[chan location.Position]struct{})
	}
	b.listeners[id][ch] = struct{}{}
	var once sync.Once
	return ch, func() {
		once.Do(func() {
			b.mu.Lock()
			defer b.mu.Unlock()
			delete(b.listeners[id], ch)
			if len(b.listeners[id]) == 0 {
				delete(b.listeners, id)
			}
			close(ch)
		})
	}
}
