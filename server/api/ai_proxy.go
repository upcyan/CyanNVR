package api

import (
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// aiWorkerURL returns the configured local detect worker base URL.
func (s *Server) aiWorkerURL() string {
	s.settingsMu.Lock()
	defer s.settingsMu.Unlock()
	return strings.TrimRight(s.settings.AI.DetectURL, "/")
}

func (s *Server) aiProxy(method, path string, body io.Reader) ([]byte, int, error) {
	req, err := http.NewRequest(method, s.aiWorkerURL()+path, body)
	if err != nil {
		return nil, 0, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	return data, resp.StatusCode, err
}

// listAIModels proxies GET /models from the detect worker.
func (s *Server) listAIModels(c *gin.Context) {
	data, code, err := s.aiProxy("GET", "/models", nil)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "AI worker unreachable"})
		return
	}
	c.Data(code, "application/json", data)
}

// loadAIModel proxies POST /load to hot-swap the active ONNX model.
func (s *Server) loadAIModel(c *gin.Context) {
	data, code, err := s.aiProxy("POST", "/load", c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "AI worker unreachable"})
		return
	}
	c.Data(code, "application/json", data)
}
