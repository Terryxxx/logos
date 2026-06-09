// Package realtime is the in-memory WebSocket hub. Every browser-side
// connection gets a *Client; the hub fans server events out to all of
// them. Single-process MVP: no Redis, no scope authorization (everyone
// connected to localhost is "the user").
package realtime

import (
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/logos-app/logos/server/pkg/protocol"
)

const (
	writeWait      = 10 * time.Second
	pongWait       = 60 * time.Second
	pingPeriod     = (pongWait * 9) / 10
	maxMessageSize = 1 << 20 // 1 MiB
)

type Client struct {
	hub  *Hub
	conn *websocket.Conn
	send chan []byte
}

type Hub struct {
	mu      sync.RWMutex
	clients map[*Client]struct{}

	register   chan *Client
	unregister chan *Client
	broadcast  chan []byte
}

func NewHub() *Hub {
	return &Hub{
		clients:    make(map[*Client]struct{}),
		register:   make(chan *Client, 16),
		unregister: make(chan *Client, 16),
		broadcast:  make(chan []byte, 256),
	}
}

func (h *Hub) Run() {
	for {
		select {
		case c := <-h.register:
			h.mu.Lock()
			h.clients[c] = struct{}{}
			h.mu.Unlock()
		case c := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[c]; ok {
				delete(h.clients, c)
				close(c.send)
			}
			h.mu.Unlock()
		case data := <-h.broadcast:
			h.mu.RLock()
			for c := range h.clients {
				select {
				case c.send <- data:
				default:
					// Slow client: drop the frame rather than block the hub.
					// In a future revision we'll close the conn instead.
				}
			}
			h.mu.RUnlock()
		}
	}
}

// Broadcast serialises a protocol envelope and queues it for all clients.
func (h *Hub) Broadcast(eventType string, payload any) {
	raw, err := json.Marshal(payload)
	if err != nil {
		slog.Warn("ws broadcast: marshal payload failed", "type", eventType, "error", err)
		return
	}
	env := protocol.Envelope{Type: eventType, Payload: raw}
	b, err := json.Marshal(env)
	if err != nil {
		slog.Warn("ws broadcast: marshal envelope failed", "type", eventType, "error", err)
		return
	}
	select {
	case h.broadcast <- b:
	default:
		slog.Warn("ws hub broadcast queue full; dropping", "type", eventType)
	}
}

// Attach turns a freshly-upgraded ws conn into a managed Client. The
// upgrade itself happens in package handler so this package stays
// http-agnostic.
func (h *Hub) Attach(conn *websocket.Conn) {
	c := &Client{hub: h, conn: conn, send: make(chan []byte, 64)}
	h.register <- c
	go c.writePump()
	go c.readPump()
}

func (c *Client) readPump() {
	defer func() {
		c.hub.unregister <- c
		_ = c.conn.Close()
	}()
	c.conn.SetReadLimit(maxMessageSize)
	_ = c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		_ = c.conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})
	for {
		// V0.1 ignores client→server frames (besides pong).
		if _, _, err := c.conn.ReadMessage(); err != nil {
			return
		}
	}
}

func (c *Client) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		_ = c.conn.Close()
	}()
	for {
		select {
		case msg, ok := <-c.send:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				_ = c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := c.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				return
			}
		case <-ticker.C:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}
