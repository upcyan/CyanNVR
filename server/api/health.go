package api

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"cyannvr/server/auth"
	"cyannvr/server/models"
)

// CoreVersion 由 main 包在启动时注入（见 main.go），用于健康检查暴露核心版本。
// 定义为变量而非常量，让 api 包不必依赖 main 包。
var CoreVersion = "dev"

func (s *Server) health(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status":  "ok",
		"name":    "CyanNVR",
		"version": CoreVersion,
		"time":    time.Now().Format(time.RFC3339),
		"auth":    true,
	})
}

func (s *Server) getSettings(c *gin.Context) {
	s.settingsMu.Lock()
	out := *s.settings
	s.settingsMu.Unlock()
	u := auth.CurrentUser(c)
	if u == nil || u.Role != models.RoleAdmin {
		out.AI.APIKey = maskKey(out.AI.APIKey)
	}
	// 只读账号（viewer）与普通用户（user）不应拿到全局管理配置：
	// 存储/录像/AI/通知/HTTPS 等都是管理员级配置，其中 AI.BaseURL、
	// DetectURL、域名、邮箱等字段对非管理角色属于越权泄露。
	// 前端个人化设置（主题/字号/关怀模式）存于 localStorage，不依赖此处，
	// 返回空配置不影响 viewer/user 正常使用界面。
	if u != nil && u.Role != models.RoleAdmin && u.Role != models.RoleOperator {
		out = AppSettings{}
	}
	c.Header("ETag", settingsETag(out))
	c.JSON(http.StatusOK, gin.H{"settings": out})
}

func maskKey(k string) string {
	if len(k) <= 6 {
		return ""
	}
	return k[:3] + "****" + k[len(k)-3:]
}

func (s *Server) putSettings(c *gin.Context) {
	s.settingsWriteMu.Lock()
	defer s.settingsWriteMu.Unlock()
	s.settingsMu.Lock()
	current := *s.settings
	s.settingsMu.Unlock()
	// Old cached clients must not blindly replace the complete configuration.
	if c.Request.Method == http.MethodPut && c.GetHeader("If-Match") == "" {
		c.JSON(http.StatusPreconditionRequired, gin.H{"error": "界面版本过旧，请刷新或升级后保存设置"})
		return
	}
	if match := c.GetHeader("If-Match"); match != "" && match != settingsETag(current) {
		c.JSON(http.StatusPreconditionFailed, gin.H{"error": "设置已被其他终端修改，请重新读取"})
		return
	}
	var in AppSettings
	if c.Request.Method == http.MethodPatch {
		body, err := readSettingsBody(c.Request.Body)
		if err == nil {
			in, err = mergeSettings(current, body)
		}
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
	} else if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}
	validModes := map[string]bool{"continuous": true, "schedule": true, "motion": true}
	if !validModes[in.RecordMode] {
		c.JSON(http.StatusBadRequest, gin.H{"error": "recordMode must be continuous/schedule/motion"})
		return
	}
	if in.RetentionDays < 1 || in.RetentionDays > 3650 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "retentionDays must be 1-3650"})
		return
	}
	if in.RetentionSizeGB < 0 || in.RetentionSizeGB > 1048576 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "retentionSizeGB must be 0-1048576"})
		return
	}
	if in.TrustWindowHours < 0 || in.TrustWindowHours > 24*365 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "trustWindowHours 必须在 0-8760 之间"})
		return
	}
	if in.HTTPSPort == 0 {
		in.HTTPSPort = 443
	}
	if in.HTTPSPort < 1 || in.HTTPSPort > 65535 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "httpsPort must be 1-65535"})
		return
	}
	switch in.TLSCertMode {
	case "", "auto", "manual":
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "tlsCertMode must be auto/manual"})
		return
	}
	switch in.AI.Provider {
	case "", "auto", "auto-bench", "cpu", "cuda", "rocm", "openvino", "directml", "tensorrt":
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "ai.provider must be auto/auto-bench/cpu/cuda/rocm/openvino/directml/tensorrt"})
		return
	}
	oldTLS := s.CurrentTLSConfig()
	s.settingsMu.Lock()
	if in.AI.APIKey != "" && len(in.AI.APIKey) < 20 && s.settings.AI.APIKey != "" {
		in.AI.APIKey = s.settings.AI.APIKey
	}
	s.settingsMu.Unlock()
	if err := s.persistSettingsValue(in); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "设置写入失败，未改变已保存配置"})
		return
	}
	s.settingsMu.Lock()
	s.settings = &in
	s.settingsMu.Unlock()
	s.applySettings()
	newTLS := s.CurrentTLSConfig()
	if newTLS != oldTLS {
		go s.tls.Apply(newTLS)
	}
	s.settingsMu.Lock()
	out := *s.settings
	s.settingsMu.Unlock()
	out.AI.APIKey = maskKey(out.AI.APIKey)
	c.Header("ETag", settingsETag(out))
	c.JSON(http.StatusOK, gin.H{"settings": out})
}

func mathRound(f float64) float64 {
	return float64(int(f*10)) / 10
}
