package api

import (
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

const (
	sseMaxClients      = 64
	sseHeartbeatPeriod = 25 * time.Second
)

type SSEHub struct {
	mu      sync.Mutex
	clients map[chan []byte]struct{}
}

func NewSSEHub() *SSEHub {
	return &SSEHub{clients: make(map[chan []byte]struct{})}
}

// Subscribe registers a new client. Returns nil when the hub is full.
func (h *SSEHub) Subscribe() chan []byte {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.clients) >= sseMaxClients {
		return nil
	}
	ch := make(chan []byte, 16)
	h.clients[ch] = struct{}{}
	return ch
}

func (h *SSEHub) Unsubscribe(ch chan []byte) {
	h.mu.Lock()
	if _, ok := h.clients[ch]; ok {
		delete(h.clients, ch)
		close(ch)
	}
	h.mu.Unlock()
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
	frame := append([]byte("data: "), data...)
	frame = append(frame, '\n', '\n')
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.clients {
		select {
		case ch <- frame:
		default:
			// Slow client: drop its buffered frames rather than block.
			for {
				select {
				case <-ch:
					continue
				default:
				}
				break
			}
		}
	}
}

func (s *Server) sseHandler(c *gin.Context) {
	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "streaming not supported"})
		return
	}

	token := c.Query("token")
	if token == "" {
		token = c.GetHeader("Authorization")
		if len(token) > 7 && token[:7] == "Bearer " {
			token = token[7:]
		}
	}
	if token == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "missing token"})
		return
	}
	if _, err := s.am.Parse(token); err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid token"})
		return
	}

	ch := s.hub.Subscribe()
	if ch == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "too many connections"})
		return
	}
	defer s.hub.Unsubscribe(ch)

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")
	c.Status(http.StatusOK)

	c.SSEvent("connected", map[string]string{"status": "ok"})
	flusher.Flush()

	notify := c.Request.Context().Done()
	heartbeat := time.NewTicker(sseHeartbeatPeriod)
	defer heartbeat.Stop()
	for {
		select {
		case <-notify:
			return
		case <-heartbeat.C:
			if _, err := c.Writer.WriteString(": ping\n\n"); err != nil {
				return
			}
			flusher.Flush()
		case data, ok := <-ch:
			if !ok {
				return
			}
			if _, err := c.Writer.Write(data); err != nil {
				return
			}
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
