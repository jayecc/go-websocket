package websocket

import (
	"bytes"
	"errors"
	"fmt"
	"log"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

const (
	// writeWait is the time allowed to write a message to the peer.
	writeWait = 10 * time.Second

	// pongWait is the time allowed to read the next pong message from the peer.
	pongWait = 60 * time.Second

	// pingPeriod is the period to send pings to peer. Must be less than pongWait.
	pingPeriod = (pongWait * 9) / 10

	// maxMessageSize is the maximum message size allowed from peer.
	maxMessageSize = 512

	// bufSize is the send buffer size for outbound messages.
	bufSize = 256
)

var (
	// newline represents a newline character as bytes.
	newline = []byte{'\n'}

	// space represents a space character as bytes.
	space = []byte{' '}
)

// upgrader is used to upgrade HTTP connections to WebSocket connections.
// It sets buffer sizes and allows all origins for CORS.
var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin:     func(r *http.Request) bool { return true },
}

// Client represents a WebSocket client connection.
// It manages the connection lifecycle, message sending/receiving, and event callbacks.
type Client struct {
	// hub manages all active clients and broadcasts messages
	hub *Hub

	// conn is the underlying WebSocket connection
	conn *websocket.Conn

	// send is a buffered channel for outbound messages
	send chan []byte

	// id uniquely identifies the client
	id string

	// Event callbacks for handling connection events
	onEvent      func(conn *Client, messageType int, message []byte)
	onConnect    func(conn *Client)
	onError      func(id string, err error)
	onDisconnect func(id string)
}

// Emit sends a message to the client.
// It returns false if the client is closed or the send buffer is full.
// This method is safe for concurrent use.
func (c *Client) Emit(message []byte) bool {
	select {
	case c.send <- message:
		return true
	default:
		// Send buffer is full, return failure
		return false
	}
}

// Broadcast sends a message to all connected clients through the hub.
// The message will be queued for delivery to all clients except the sender.
func (c *Client) Broadcast(message []byte) {
	c.hub.Broadcast(message)
}

// GetID returns the unique identifier of this client.
// The ID is generated when the client is created and remains constant
// throughout the lifetime of the connection.
func (c *Client) GetID() string {
	return c.id
}

// reader reads messages from the WebSocket connection in a loop.
// It handles pong messages and processes incoming messages.
// If any error occurs, it unregisters the client from the hub.
func (c *Client) reader() {
	defer func() {
		c.hub.unregister <- c
		if err := recover(); err != nil {
			c.error(fmt.Errorf("panic: %v", err))
		}
	}()

	// Set message size limit
	c.conn.SetReadLimit(maxMessageSize)

	// Set initial read timeout
	if err := c.conn.SetReadDeadline(time.Now().Add(pongWait)); err != nil {
		c.error(err)
		return
	}

	// Set pong handler
	c.conn.SetPongHandler(func(string) error {
		return c.conn.SetReadDeadline(time.Now().Add(pongWait))
	})

	for {
		messageType, message, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseNormalClosure) {
				c.error(err)
			}
			return
		}
		if c.onEvent != nil {
			message = bytes.TrimSpace(bytes.Replace(message, newline, space, -1))
			c.onEvent(c, messageType, message)
		}
	}
}

// writer handles writing messages to the WebSocket connection.
// It sends messages from the send channel and pings the client periodically.
// It ensures proper cleanup when the writer stops.
func (c *Client) writer() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		if c.onDisconnect != nil {
			c.onDisconnect(c.id)
		}
		if err := recover(); err != nil {
			c.error(fmt.Errorf("panic: %v", err))
		}
	}()

	for {
		select {
		case message, ok := <-c.send:
			// Set write timeout
			if err := c.conn.SetWriteDeadline(time.Now().Add(writeWait)); err != nil {
				c.error(err)
				return
			}

			if !ok {
				// Channel is closed, send close message
				if err := c.conn.WriteMessage(websocket.CloseMessage, []byte{}); err != nil && !errors.Is(err, websocket.ErrCloseSent) {
					c.error(err)
				}
				return
			}

			// Get writer
			w, err := c.conn.NextWriter(websocket.TextMessage)
			if err != nil {
				c.error(err)
				return
			}

			// Write current message
			if _, err := w.Write(message); err != nil {
				c.error(err)
				return
			}

			// Write other messages from queue
			n := len(c.send)
			for i := 0; i < n; i++ {
				if _, err := w.Write(newline); err != nil {
					c.error(err)
					return
				}
				msg := <-c.send
				if _, err := w.Write(msg); err != nil {
					c.error(err)
					return
				}
			}

			// Close writer
			if err := w.Close(); err != nil {
				c.error(err)
				return
			}

		case <-ticker.C:
			// Send ping message
			if err := c.conn.SetWriteDeadline(time.Now().Add(writeWait)); err != nil {
				c.error(err)
				return
			}
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				c.error(err)
				return
			}
		}
	}
}

// Option is a function that configures a Client.
// It follows the functional options pattern for flexible client configuration.
type Option func(c *Client)

// WithID sets the client ID.
// If not provided, a UUID will be generated automatically.
// @Description: Set client ID
// @param id The client ID to set
// @return Option A function that sets the client ID
func WithID(id string) Option {
	return func(c *Client) {
		c.id = id
	}
}

// NewClient creates a new Client instance with the given hub and options.
// If no ID is provided via options, a UUID will be generated.
// The client is not connected until Conn() is called.
// @Description: Create client
// @param hub The hub to register the client with
// @param opts Optional configuration functions
// @return *Client A new Client instance
func NewClient(hub *Hub, opts ...Option) *Client {

	cli := &Client{
		hub:  hub,
		send: make(chan []byte, bufSize),
	}

	for _, opt := range opts {
		opt(cli)
	}

	if cli.id == "" {
		cli.id = strings.ReplaceAll(uuid.NewString(), "-", "")
	}

	return cli
}

// Conn upgrades the HTTP connection to a WebSocket connection and starts the client.
// It handles:
// - Upgrading the HTTP connection to WebSocket
// - Registering the client with the hub
// - Starting reader and writer goroutines
// - Triggering the OnConnect callback
//
// Parameters:
// - w: the HTTP response writer
// - r: the HTTP request
//
// Returns an error if the connection upgrade fails.
func (c *Client) Conn(w http.ResponseWriter, r *http.Request) error {
	// Upgrade HTTP connection to WebSocket
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return err
	}

	// Set connection and register client
	c.conn = conn
	c.hub.register <- c

	// Start reader and writer goroutines
	go c.writer()
	go c.reader()

	// Trigger connect callback
	if c.onConnect != nil {
		c.onConnect(c)
	}

	return nil
}

// error handles errors for the client by calling the error callback or logging.
func (c *Client) error(err error) {
	if c.onError != nil {
		c.onError(c.id, err)
	} else {
		log.Printf("error: %s %v", c.id, err)
		debug.PrintStack()
	}
}

// OnEvent sets the callback for handling incoming WebSocket messages.
// The callback receives:
// - conn: the client that received the message
// - messageType: the WebSocket message type (text/binary)
// - message: the message payload
func (c *Client) OnEvent(handler func(conn *Client, messageType int, message []byte)) {
	c.onEvent = handler
}

// OnConnect sets the callback for when a WebSocket connection is established.
// The callback receives:
// - conn: the newly connected client
func (c *Client) OnConnect(handler func(conn *Client)) {
	c.onConnect = handler
}

// OnError sets the callback for handling WebSocket errors.
// The callback receives:
// - id: the client ID where the error occurred
// - err: the error that occurred
func (c *Client) OnError(handler func(id string, err error)) {
	c.onError = handler
}

// OnDisconnect sets the callback for when a WebSocket connection is closed.
// The callback receives:
// - id: the ID of the client that disconnected
func (c *Client) OnDisconnect(handler func(id string)) {
	c.onDisconnect = handler
}
