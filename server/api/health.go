package api

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"cyannvr/server/auth"
	"cyannvr/server/models"
)

func (s *Server) health(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status": "ok",
		"name":   "CyanNVR",
		"time":   time.Now().Format(time.RFC3339),
		"auth":   true,
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
	c.JSON(http.StatusOK, gin.H{"settings": out})
}

func maskKey(k string) string {
	if len(k) <= 6 {
		return ""
	}
	return k[:3] + "****" + k[len(k)-3:]
}

func (s *Server) putSettings(c *gin.Context) {
	var in AppSettings
	if err := c.ShouldBindJSON(&in); err != nil {
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
	oldTLS := s.CurrentTLSConfig()
	s.settingsMu.Lock()
	if in.AI.APIKey != "" && len(in.AI.APIKey) < 20 && s.settings.AI.APIKey != "" {
		in.AI.APIKey = s.settings.AI.APIKey
	}
	s.settings = &in
	s.settingsMu.Unlock()
	s.saveSettings()
	s.applySettings()
	newTLS := s.CurrentTLSConfig()
	if newTLS != oldTLS {
		go s.tls.Apply(newTLS)
	}
	s.settingsMu.Lock()
	out := *s.settings
	s.settingsMu.Unlock()
	out.AI.APIKey = maskKey(out.AI.APIKey)
	c.JSON(http.StatusOK, gin.H{"settings": out})
}

func mathRound(f float64) float64 {
	return float64(int(f*10)) / 10
}
