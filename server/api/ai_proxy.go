package api

import (
	"bytes"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"cyannvr/server/pkg/ai"
)

// aiWorkerURL returns the configured local detect worker base URL.
func (s *Server) aiWorkerURL() string {
	s.settingsMu.Lock()
	defer s.settingsMu.Unlock()
	return strings.TrimRight(s.settings.AI.DetectURL, "/")
}

func (s *Server) aiProxy(method, path string, body io.Reader) ([]byte, int, error) {
	base := s.aiWorkerURL()

	// 必须把请求体先读成 []byte 再用 bytes.Reader 传进去。
	// 直接传 gin 的 c.Request.Body（io.ReadCloser）时，Go 的 http 客户端
	// 无法推断长度，会改用 chunked transfer-encoding；而检测进程用的是
	// Python BaseHTTPRequestHandler，它不解析 chunked 请求体，
	// 结果 Content-Length 视为 0，body 变空并报 JSON 解析错误。
	var payload []byte
	if body != nil {
		var err error
		payload, err = io.ReadAll(io.LimitReader(body, 4<<20))
		if err != nil {
			return nil, 0, err
		}
	}

	// 复用 ai 包的地址归一化与客户端构造：检测进程默认走 Unix Domain Socket，
	// 若此处仍按 http:// 拼 URL，会得到 unix:/path/models 这类无效地址而返回 502
	var rdr io.Reader
	if payload != nil {
		rdr = bytes.NewReader(payload)
	}
	req, err := http.NewRequest(method, ai.DetectEndpoint(base)+strings.TrimPrefix(path, "/"), rdr)
	if err != nil {
		return nil, 0, err
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
		req.ContentLength = int64(len(payload))
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

// downloadAIModel proxies POST /download so the UI can fetch a catalog model.
// 模型体积可达上百 MB，因此这里单独放宽超时与响应体上限。
func (s *Server) downloadAIModel(c *gin.Context) {
	base := s.aiWorkerURL()
	// 同 aiProxy：请求体必须先落地成 []byte，否则会退化成 chunked，
	// 而 Python 端的 BaseHTTPRequestHandler 读不到 chunked body。
	payload, err := io.ReadAll(io.LimitReader(c.Request.Body, 1<<20))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	req, err := http.NewRequest("POST", ai.DetectEndpoint(base)+"download", bytes.NewReader(payload))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.ContentLength = int64(len(payload))
	client := ai.NewDetectClient(base)
	client.Timeout = 10 * time.Minute
	resp, err := client.Do(req)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "AI worker unreachable"})
		return
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	c.Data(resp.StatusCode, "application/json", data)
}

// aiWorkerStatus proxies GET / from the detect worker: engine, active model,
// 实际生效的推理后端（cpu/cuda/...），以及发生回退时的原因。
func (s *Server) aiWorkerStatus(c *gin.Context) {
	data, code, err := s.aiProxy("GET", "/", nil)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "AI worker unreachable"})
		return
	}
	c.Data(code, "application/json", data)
}
