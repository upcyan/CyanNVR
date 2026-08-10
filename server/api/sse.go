package api

import (
	"encoding/json"
	"log"
	"net/http"
	"sync"

	"github.com/gin-gonic/gin"
)

type SSEHub struct {
	mu      sync.Mutex
	clients map[chan []byte]struct{}
}

func NewSSEHub() *SSEHub {
	return &SSEHub{clients: make(map[chan []byte]struct{})}
}

func (h *SSEHub) Subscribe() chan []byte {
	ch := make(chan []byte, 16)
	h.mu.Lock()
	h.clients[ch] = struct{}{}
	h.mu.Unlock()
	return ch
}

func (h *SSEHub) Unsubscribe(ch chan []byte) {
	h.mu.Lock()
	delete(h.clients, ch)
	h.mu.Unlock()
	close(ch)
}

type Notification struct {
	Type       string `json:"type"`
	DeviceID   string `json:"deviceId"`
	DeviceName string `json:"deviceName"`
	EventID    string `json:"eventId,omitempty"`
	Label      string `json:"label,omitempty"`
	Desc       string `json:"description,omitempty"`
	Time       string `json:"time"`
}

func (h *SSEHub) Broadcast(n Notification) {
	data, err := json.Marshal(n)
	if err != nil {
		return
	}
	data = append([]byte("data: "), data...)
	data = append(data, '\n', '\n')
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.clients {
		select {
		case ch <- data:
		default:
		}
	}
}

func (s *Server) sseHandler(c *gin.Context) {
	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "streaming not supported"})
		return
	}
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")
	c.Status(http.StatusOK)
	flusher.Flush()

	token := c.Query("token")
	if token == "" {
		return
	}
	if _, err := s.am.Parse(token); err != nil {
		return
	}

	ch := s.hub.Subscribe()
	defer s.hub.Unsubscribe(ch)

	c.SSEvent("connected", map[string]string{"status": "ok"})
	flusher.Flush()

	notify := c.Request.Context().Done()
	for {
		select {
		case <-notify:
			return
		case data, ok := <-ch:
			if !ok {
				return
			}
			c.Writer.Write(data)
			flusher.Flush()
		}
	}
}

func (s *Server) BroadcastEvent(n Notification) {
	if s.hub == nil {
		return
	}
	log.Printf("[SSE] %s %s %s", n.Type, n.DeviceName, n.Label)
	s.hub.Broadcast(n)
}
