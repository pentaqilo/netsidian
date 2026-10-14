package service

import (
	"context"
	"sync"
	"time"

	"github.com/pentaqilo/netsidian/internal/domain"
)

// EventHub is an in-process fanout for authorized invalidation metadata.
// It carries no private bodies; SSE handlers recheck membership before
// delivering anything and clients refetch authorized content.
type EventHub struct {
	mu   sync.Mutex
	subs map[chan *domain.Event]bool
}

func NewEventHub() *EventHub {
	return &EventHub{subs: map[chan *domain.Event]bool{}}
}

func (h *EventHub) Subscribe() chan *domain.Event {
	h.mu.Lock()
	defer h.mu.Unlock()
	ch := make(chan *domain.Event, 32)
	h.subs[ch] = true
	return ch
}

func (h *EventHub) Unsubscribe(ch chan *domain.Event) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.subs[ch] {
		close(ch)
		delete(h.subs, ch)
	}
}

func (h *EventHub) publish(e *domain.Event) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.subs {
		select {
		case ch <- e:
		default: // slow consumer: client will catch up from the outbox
		}
	}
}

// PublishEvent appends to the durable outbox and fans out live.
func (s *Services) PublishEvent(e *domain.Event) {
	if e.EventType == "" {
		return
	}
	if e.CreatedAt.IsZero() {
		e.CreatedAt = time.Now()
	}
	seq, err := s.Docs.AppendEvent(context.Background(), e)
	if err != nil {
		return
	}
	e.Seq = seq
	s.Hub(e.SpaceID).publish(e)
}
