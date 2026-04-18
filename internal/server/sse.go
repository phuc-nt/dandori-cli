package server

import (
	"encoding/json"
	"sync"
)

type SSEHub struct {
	clients map[chan string]bool
	mu      sync.RWMutex
}

func NewSSEHub() *SSEHub {
	return &SSEHub{
		clients: make(map[chan string]bool),
	}
}

func (h *SSEHub) AddClient() chan string {
	h.mu.Lock()
	defer h.mu.Unlock()

	ch := make(chan string, 10)
	h.clients[ch] = true
	return ch
}

func (h *SSEHub) RemoveClient(ch chan string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	delete(h.clients, ch)
	close(ch)
}

func (h *SSEHub) Broadcast(data any) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	msg, _ := json.Marshal(data)
	for ch := range h.clients {
		select {
		case ch <- string(msg):
		default:
		}
	}
}

func (h *SSEHub) ClientCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}
