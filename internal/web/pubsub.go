package web

import (
	"sync"

	"github.com/colormechadd/mailaroo/internal/pipeline"
	"github.com/google/uuid"
)

type Hub struct {
	mu          sync.RWMutex
	subscribers map[uuid.UUID][]chan pipeline.Event
}

func NewHub() *Hub {
	return &Hub{
		subscribers: make(map[uuid.UUID][]chan pipeline.Event),
	}
}

func (h *Hub) Subscribe(userID uuid.UUID) chan pipeline.Event {
	h.mu.Lock()
	defer h.mu.Unlock()

	ch := make(chan pipeline.Event, 10)
	h.subscribers[userID] = append(h.subscribers[userID], ch)
	return ch
}

func (h *Hub) Unsubscribe(userID uuid.UUID, ch chan pipeline.Event) {
	h.mu.Lock()
	defer h.mu.Unlock()

	subs := h.subscribers[userID]
	for i, sub := range subs {
		if sub == ch {
			h.subscribers[userID] = append(subs[:i], subs[i+1:]...)
			close(ch)
			break
		}
	}
}

func (h *Hub) Broadcast(event pipeline.Event) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if subs, ok := h.subscribers[event.UserID]; ok {
		for _, ch := range subs {
			// Non-blocking send
			select {
			case ch <- event:
			default:
			}
		}
	}
}
