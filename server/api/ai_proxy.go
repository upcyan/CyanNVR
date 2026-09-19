package api

import (
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"simplenvr/server/pkg/ai"
)

// aiWorkerURL returns the configured local detect worker base URL.
func (s *Server) aiWorkerURL() string {
	s.settingsMu.Lock()
	defer s.settingsMu.Unlock()
	return strings.TrimRight(s.settings.AI.DetectURL, "/")
}

func (s *Server) aiProxy(method, path string, body io.Reader) ([]byte, int, error) {
	base := s.aiWorkerURL()
	// 复用 ai 包的地址归一化与客户端构造：检测进程默认走 Unix Domain Socket，
	// 若此处仍按 http:// 拼 URL，会得到 unix:/path/models 这类无效地址而返回 502
	req, err := http.NewRequest(method, ai.DetectEndpoint(base)+strings.TrimPrefix(path, "/"), body)
	if err != nil {
		return nil, 0, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	client := ai.NewDetectClient(base)
	client.Timeout = 10 * time.Second
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 1MB limit
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
