package webui

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/log"
)

// SSEClient represents a connected SSE client.
type SSEClient struct {
	ID       string
	Messages chan []byte
}

// SSEBroker manages Server-Sent Events connections and broadcasts.
type SSEBroker struct {
	clients    map[string]*SSEClient
	register   chan *SSEClient
	unregister chan *SSEClient
	broadcast  chan []byte
	mu         sync.RWMutex
}

// NewSSEBroker creates a new SSE broker instance.
func NewSSEBroker() *SSEBroker {
	return &SSEBroker{
		clients:    make(map[string]*SSEClient),
		register:   make(chan *SSEClient),
		unregister: make(chan *SSEClient),
		broadcast:  make(chan []byte, 100),
	}
}

// Start starts the SSE broker event loop.
func (b *SSEBroker) Start(ctx context.Context) {
	logger := log.FromContext(ctx).WithName("sse")

	for {
		select {
		case <-ctx.Done():
			logger.Info("shutting down sse broker")
			b.mu.Lock()
			for _, client := range b.clients {
				close(client.Messages)
			}
			b.clients = make(map[string]*SSEClient)
			b.mu.Unlock()
			return

		case client := <-b.register:
			b.mu.Lock()
			b.clients[client.ID] = client
			b.mu.Unlock()
			logger.Info("sse client registered", "id", client.ID, "total", len(b.clients))

		case client := <-b.unregister:
			b.mu.Lock()
			if _, ok := b.clients[client.ID]; ok {
				close(client.Messages)
				delete(b.clients, client.ID)
			}
			b.mu.Unlock()
			logger.Info("sse client unregistered", "id", client.ID, "total", len(b.clients))

		case message := <-b.broadcast:
			b.mu.RLock()
			for _, client := range b.clients {
				select {
				case client.Messages <- message:
				default:
					logger.Info("client message queue full, skipping", "id", client.ID)
				}
			}
			b.mu.RUnlock()
		}
	}
}

// ServeHTTP handles SSE connection requests.
func (b *SSEBroker) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	// Check if response writer supports flushing
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	// Create new client
	client := &SSEClient{
		ID:       generateClientID(),
		Messages: make(chan []byte, 10),
	}

	// Register client
	b.register <- client

	// Ensure client is unregistered on disconnect
	defer func() {
		b.unregister <- client
	}()

	// Send initial connection message
	if _, err := fmt.Fprintf(w, "data: connected\n\n"); err != nil {
		return
	}
	flusher.Flush()

	// Stream messages to client
	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return

		case message, ok := <-client.Messages:
			if !ok {
				return
			}
			if _, err := w.Write(message); err != nil {
				return
			}
			flusher.Flush()

		case <-time.After(30 * time.Second):
			// Send keep-alive ping
			if _, err := fmt.Fprintf(w, ": ping\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

// Broadcast sends a message to all connected clients.
func (b *SSEBroker) Broadcast(message []byte) {
	select {
	case b.broadcast <- message:
	default:
		// Broadcast queue full, skip message
	}
}

// BroadcastEvent sends a named SSE event to all clients.
func (b *SSEBroker) BroadcastEvent(eventName string, data string) {
	message := fmt.Sprintf("event: %s\ndata: %s\n\n", eventName, data)
	b.Broadcast([]byte(message))
}

// ClientCount returns the number of connected clients.
func (b *SSEBroker) ClientCount() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.clients)
}

// generateClientID generates a unique client identifier.
func generateClientID() string {
	return fmt.Sprintf("client-%d", time.Now().UnixNano())
}
