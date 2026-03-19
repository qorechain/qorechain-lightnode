package dashboard

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"golang.org/x/net/websocket"
)

const (
	// writeWait is the maximum time to wait for a write to complete.
	writeWait = 10 * time.Second

	// pongWait is the maximum time to wait for a pong response.
	pongWait = 60 * time.Second

	// pingPeriod sends pings at this interval (must be < pongWait).
	pingPeriod = 50 * time.Second

	// maxMessageSize limits incoming message size.
	maxMessageSize = 4096

	// sendBufferSize is the channel buffer for outbound messages.
	sendBufferSize = 64
)

// Hub manages WebSocket connections and broadcasts updates to all
// connected dashboard clients.
type Hub struct {
	clients map[*wsClient]bool
	mu      sync.RWMutex
	logger  *slog.Logger
}

// wsClient represents a single WebSocket connection.
type wsClient struct {
	hub  *Hub
	conn *websocket.Conn
	send chan []byte
}

// NewHub creates a new WebSocket hub.
func NewHub(logger *slog.Logger) *Hub {
	return &Hub{
		clients: make(map[*wsClient]bool),
		logger:  logger,
	}
}

// HandleWS upgrades an HTTP request to a WebSocket connection.
func (h *Hub) HandleWS(w http.ResponseWriter, r *http.Request) {
	handler := websocket.Handler(func(conn *websocket.Conn) {
		c := &wsClient{
			hub:  h,
			conn: conn,
			send: make(chan []byte, sendBufferSize),
		}

		h.register(c)
		defer h.unregister(c)

		// Start write pump in background
		done := make(chan struct{})
		go func() {
			c.writePump()
			close(done)
		}()

		// Read pump blocks until connection closes
		c.readPump()

		// Wait for write pump to finish
		<-done
	})

	handler.ServeHTTP(w, r)
}

// Broadcast sends a raw message to all connected clients.
func (h *Hub) Broadcast(msg []byte) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	for c := range h.clients {
		select {
		case c.send <- msg:
		default:
			// Client send buffer full; drop the message.
			h.logger.Debug("dropping message for slow client")
		}
	}
}

// BroadcastJSON marshals v to JSON and broadcasts it.
func (h *Hub) BroadcastJSON(v interface{}) {
	data, err := json.Marshal(v)
	if err != nil {
		h.logger.Warn("failed to marshal broadcast payload", "error", err)
		return
	}
	h.Broadcast(data)
}

// ClientCount returns the number of connected clients.
func (h *Hub) ClientCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}

func (h *Hub) register(c *wsClient) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.clients[c] = true
	h.logger.Debug("websocket client connected", "clients", len(h.clients))
}

func (h *Hub) unregister(c *wsClient) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.clients[c]; ok {
		delete(h.clients, c)
		close(c.send)
		h.logger.Debug("websocket client disconnected", "clients", len(h.clients))
	}
}

// readPump reads messages from the client. Currently only used to
// detect disconnection and keep the connection alive.
func (c *wsClient) readPump() {
	defer c.conn.Close()

	for {
		var msg []byte
		if err := websocket.Message.Receive(c.conn, &msg); err != nil {
			// Connection closed or read error
			break
		}
		// Future: handle incoming commands from the dashboard UI.
		// For now, messages from the client are ignored.
	}
}

// writePump sends messages from the send channel to the WebSocket.
func (c *wsClient) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()

	for {
		select {
		case msg, ok := <-c.send:
			if !ok {
				// Hub closed the channel — client was unregistered.
				return
			}
			c.conn.SetWriteDeadline(time.Now().Add(writeWait)) //nolint:errcheck
			if _, err := c.conn.Write(msg); err != nil {
				return
			}
		case <-ticker.C:
			// Send a ping frame to keep the connection alive.
			c.conn.SetWriteDeadline(time.Now().Add(writeWait)) //nolint:errcheck
			if _, err := c.conn.Write([]byte(`{"type":"ping"}`)); err != nil {
				return
			}
		}
	}
}
