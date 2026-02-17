package www

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"
)

// SSEEvent represents an event to broadcast to SSE clients.
type SSEEvent struct {
	Type string      `json:"type"`
	Data interface{} `json:"data"`
}

type sseClient struct {
	id     string
	events chan SSEEvent
	done   chan struct{}
}

// EventHub manages SSE client connections and broadcasts events.
type EventHub struct {
	clients    map[string]*sseClient
	register   chan *sseClient
	unregister chan *sseClient
	broadcast  chan SSEEvent
	mu         sync.RWMutex
	done       chan struct{}
}

func newEventHub() *EventHub {
	hub := &EventHub{
		clients:    make(map[string]*sseClient),
		register:   make(chan *sseClient),
		unregister: make(chan *sseClient),
		broadcast:  make(chan SSEEvent, 256),
		done:       make(chan struct{}),
	}
	go hub.run()
	return hub
}

func (h *EventHub) run() {
	for {
		select {
		case client := <-h.register:
			h.mu.Lock()
			h.clients[client.id] = client
			h.mu.Unlock()

		case client := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[client.id]; ok {
				delete(h.clients, client.id)
				close(client.events)
			}
			h.mu.Unlock()

		case event := <-h.broadcast:
			h.mu.RLock()
			for _, client := range h.clients {
				select {
				case client.events <- event:
				default:
				}
			}
			h.mu.RUnlock()

		case <-h.done:
			h.mu.Lock()
			for id, client := range h.clients {
				close(client.events)
				delete(h.clients, id)
			}
			h.mu.Unlock()
			return
		}
	}
}

// Stop stops the EventHub.
func (h *EventHub) Stop() {
	close(h.done)
}

// Broadcast sends an event to all connected clients.
func (h *EventHub) Broadcast(event SSEEvent) {
	select {
	case h.broadcast <- event:
	default:
		log.Printf("[SSE] broadcast channel full, dropping %s event", event.Type)
	}
}

// ClientCount returns the number of connected clients.
func (h *EventHub) ClientCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}

func (h *Handlers) handleSSE(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "SSE not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	clientID := fmt.Sprintf("client-%d", time.Now().UnixNano())
	client := &sseClient{
		id:     clientID,
		events: make(chan SSEEvent, 64),
		done:   make(chan struct{}),
	}

	h.eventHub.register <- client

	notify := r.Context().Done()

	fmt.Fprintf(w, "event: connected\ndata: {\"id\":\"%s\"}\n\n", clientID)
	flusher.Flush()

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-notify:
			h.eventHub.unregister <- client
			return

		case event, ok := <-client.events:
			if !ok {
				return
			}
			data, err := json.Marshal(event.Data)
			if err != nil {
				continue
			}
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event.Type, string(data))
			flusher.Flush()

		case <-ticker.C:
			fmt.Fprintf(w, ": keepalive\n\n")
			flusher.Flush()
		}
	}
}
