package api

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"simplenvr/server/auth"
	"simplenvr/server/models"
	"simplenvr/server/pkg/ffmpeg"
)

func (s *Server) health(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status":    "ok",
		"name":      "SimpleNVR",
		"time":      time.Now().Format(time.RFC3339),
		"ffmpeg":    ffmpeg.Exists(s.cfg.Ffmpeg),
		"aiEnabled": s.cfg.AIEnabled,
		"auth":      true,
	})
}

func (s *Server) getSettings(c *gin.Context) {
	out := *s.settings
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
	if in.AI.APIKey != "" && len(in.AI.APIKey) < 20 && s.settings.AI.APIKey != "" {
		in.AI.APIKey = s.settings.AI.APIKey
	}
	s.settings = &in
	s.saveSettings()
	s.applySettings()
	c.JSON(http.StatusOK, gin.H{"settings": *s.settings})
}

func mathRound(f float64) float64 {
	return float64(int(f*10)) / 10
}
