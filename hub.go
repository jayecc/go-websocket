package websocket

import (
	"log"
	"sync"
)

// Hub maintains the set of active clients and broadcasts messages to the clients.
// It acts as the central message broker for the WebSocket server.
// It is safe for concurrent use by multiple goroutines.
type Hub struct {
	// clientsLock protects the clients map from concurrent access
	clientsLock sync.RWMutex

	// clients is a map of registered clients keyed by their ID
	clients map[string]*Client

	// broadcast is a channel for inbound messages from clients to be broadcast
	broadcast chan []byte

	// register is a channel for client registration requests
	register chan *Client

	// unregister is a channel for client unregistration requests
	unregister chan *Client

	// done is a channel for signaling graceful shutdown
	done chan struct{}
}

// NewHub creates a new Hub instance with initialized channels and maps.
// The hub is not started until Run() is called.
func NewHub() *Hub {
	h := &Hub{
		broadcast:   make(chan []byte, 100),
		register:    make(chan *Client, 100),
		unregister:  make(chan *Client, 100),
		clients:     make(map[string]*Client),
		clientsLock: sync.RWMutex{},
		done:        make(chan struct{}),
	}
	return h
}

// NewHubRun creates a new Hub and starts running it in a separate goroutine.
// This is a convenience function that combines NewHub() and Run().
func NewHubRun() *Hub {
	h := NewHub()
	go h.Run()
	return h
}

// Client retrieves a client by ID.
// It returns the client and a boolean indicating if the client was found.
// This method is safe for concurrent use.
func (h *Hub) Client(id string) (*Client, bool) {
	h.clientsLock.RLock()
	defer h.clientsLock.RUnlock()
	cli, ok := h.clients[id]
	return cli, ok
}

// Close gracefully shuts down the hub by closing the done channel.
// This signals the Run() loop to stop and clean up resources.
func (h *Hub) Close() {
	close(h.done)
}

// Broadcast sends a message to all connected clients.
// The message is queued for delivery and sent asynchronously.
// This method returns immediately, even if clients cannot receive the message.
func (h *Hub) Broadcast(message []byte) {
	select {
	case h.broadcast <- message:
	case <-h.done:
		return
	}
}

// Run starts the hub's main event loop.
// This function handles client registration, unregistration, and message broadcasting.
// It should be called in a separate goroutine or use NewHubRun() instead.
// The loop continues until Close() is called.
func (h *Hub) Run() {
	defer func() {
		if err := recover(); err != nil {
			log.Printf("panic: %v", err)
		}

		// Close all client connections
		h.clientsLock.Lock()
		for _, client := range h.clients {
			delete(h.clients, client.id)
			close(client.send)
			_ = client.conn.Close()
		}
		h.clients = make(map[string]*Client)
		h.clientsLock.Unlock()

		// Close all channels
		close(h.broadcast)
		close(h.register)
		close(h.unregister)
	}()

	for {
		select {
		case <-h.done:
			return
		case client := <-h.register:
			h.clientsLock.Lock()
			h.clients[client.id] = client
			h.clientsLock.Unlock()
		case client := <-h.unregister:
			h.clientsLock.Lock()
			if _, ok := h.clients[client.id]; ok {
				delete(h.clients, client.id)
				close(client.send)
				_ = client.conn.Close()
			}
			h.clientsLock.Unlock()
		case message := <-h.broadcast:
			h.clientsLock.RLock()
			clients := make([]*Client, 0, len(h.clients))
			for _, client := range h.clients {
				clients = append(clients, client)
			}
			h.clientsLock.RUnlock()
			for _, client := range clients {
				select {
				case client.send <- message:
				default:
					// If client cannot receive message, it may have disconnected, remove it
					h.clientsLock.Lock()
					delete(h.clients, client.id)
					h.clientsLock.Unlock()
					close(client.send)
					_ = client.conn.Close()
				}
			}
		}
	}
}
