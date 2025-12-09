package server

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
)

// WebSocket upgrader with CORS support
var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		// Allow all origins for development
		// TODO: Configure appropriately for production
		return true
	},
}

// ProgressUpdate represents a real-time progress update message.
type ProgressUpdate struct {
	Type      string    `json:"type"` // download, cache, error
	MediaID   string    `json:"media_id"`
	Title     string    `json:"title,omitempty"`
	Progress  float64   `json:"progress"`        // 0-100
	Speed     int64     `json:"speed,omitempty"` // bytes per second
	ETA       string    `json:"eta,omitempty"`   // estimated time remaining
	Status    string    `json:"status"`          // queued, downloading, completed, failed
	Message   string    `json:"message,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}

// WebSocketClient represents a connected WebSocket client.
type WebSocketClient struct {
	conn   *websocket.Conn
	send   chan ProgressUpdate
	server *Server
	logger *slog.Logger
}

// handleWebSocket handles WebSocket connections for real-time progress updates.
// Clients receive live updates about download progress, cache operations, and system status.
func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	// Upgrade HTTP connection to WebSocket
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		s.logger.Error("Failed to upgrade WebSocket connection", "error", err)
		return
	}

	// Create client
	client := &WebSocketClient{
		conn:   conn,
		send:   make(chan ProgressUpdate, 256),
		server: s,
		logger: s.logger,
	}

	s.logger.Info("WebSocket client connected", "remote_addr", r.RemoteAddr)

	// Register client
	s.registerWSClient(client)

	// Start client goroutines
	go client.writePump()
	go client.readPump()

	// Send initial status
	client.sendInitialStatus()
}

// writePump handles sending messages to the WebSocket client.
// Runs in a goroutine and manages connection cleanup on error.
func (c *WebSocketClient) writePump() {
	ticker := time.NewTicker(54 * time.Second)
	defer func() {
		ticker.Stop()
		c.conn.Close()
		c.server.unregisterWSClient(c)
		c.logger.Debug("WebSocket write pump stopped")
	}()

	for {
		select {
		case update, ok := <-c.send:
			// Set write deadline
			c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))

			if !ok {
				// Channel closed
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			// Send progress update
			if err := c.conn.WriteJSON(update); err != nil {
				c.logger.Error("WebSocket write error", "error", err)
				return
			}

		case <-ticker.C:
			// Send ping to keep connection alive
			c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				c.logger.Error("WebSocket ping error", "error", err)
				return
			}
		}
	}
}

// readPump handles reading messages from the WebSocket client.
// Processes client commands and maintains connection health.
func (c *WebSocketClient) readPump() {
	defer func() {
		c.conn.Close()
		close(c.send)
		c.logger.Debug("WebSocket read pump stopped")
	}()

	// Set connection limits
	c.conn.SetReadLimit(512)
	c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	for {
		// Read message from client
		messageType, message, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				c.logger.Error("WebSocket read error", "error", err)
			}
			return
		}

		// Handle different message types
		switch messageType {
		case websocket.TextMessage:
			c.handleTextMessage(message)
		case websocket.BinaryMessage:
			c.logger.Warn("Binary messages not supported")
		}

		// Reset read deadline
		c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	}
}

// handleTextMessage processes text messages from WebSocket clients.
// Supports commands like subscribe/unsubscribe from specific media updates.
func (c *WebSocketClient) handleTextMessage(message []byte) {
	c.logger.Debug("WebSocket message received", "message", string(message))

	// TODO: Implement client commands when needed
	// Examples: subscribe to specific media ID, request status update, etc.
}

// sendInitialStatus sends the current system status to a newly connected client.
func (c *WebSocketClient) sendInitialStatus() {
	// Get current cache stats
	cacheStats, err := c.server.storage.GetCacheStats()
	if err != nil {
		c.logger.Error("Failed to get cache stats", "error", err)
		return
	}

	// Calculate max size from storage stats (StorageStats has MaxSize field)
	storageStats, err := c.server.storage.GetStorageStats()
	maxSize := int64(10 * 1024 * 1024 * 1024) // Default 10GB if storage stats unavailable
	if err == nil && storageStats.MaxSize > 0 {
		maxSize = storageStats.MaxSize
	}

	// Send initial status update
	update := ProgressUpdate{
		Type:      "status",
		Progress:  float64(cacheStats.TotalSizeBytes) / float64(maxSize) * 100,
		Status:    "connected",
		Message:   "WebSocket connected successfully",
		Timestamp: time.Now(),
	}

	select {
	case c.send <- update:
	default:
		c.logger.Warn("Failed to send initial status - channel full")
	}
}

// BroadcastProgressUpdate sends a progress update to all connected WebSocket clients.
// This method will be called by the download manager to notify clients of updates.
func (s *Server) BroadcastProgressUpdate(update ProgressUpdate) {
	update.Timestamp = time.Now()

	s.wsMutex.RLock()
	clients := make([]*WebSocketClient, 0, len(s.wsClients))
	for client := range s.wsClients {
		if wsClient, ok := client.(*WebSocketClient); ok {
			clients = append(clients, wsClient)
		}
	}
	s.wsMutex.RUnlock()

	s.logger.Debug("Broadcasting progress update",
		"type", update.Type,
		"media_id", update.MediaID,
		"progress", update.Progress,
		"client_count", len(clients))

	// Send to all clients
	for _, client := range clients {
		select {
		case client.send <- update:
		default:
			client.logger.Warn("Failed to send broadcast - client channel full")
		}
	}
}

// SendProgressToClient sends a progress update to a specific client.
// Used for targeted updates when clients subscribe to specific media items.
func (c *WebSocketClient) SendProgress(update ProgressUpdate) {
	update.Timestamp = time.Now()

	select {
	case c.send <- update:
	default:
		c.logger.Warn("Failed to send progress update - channel full", "media_id", update.MediaID)
	}
}
