package api

import (
	"net/http"
	"os"
	"path/filepath"

	"github.com/gin-gonic/gin"

	"simplenvr/server/auth"
	"simplenvr/server/models"
)

// streamAuth allows token via header or query (for <video>/hls native playback).
func (s *Server) streamAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		token := c.Query("token")
		if token == "" {
			token = c.GetHeader("Authorization")
			if len(token) > 7 && token[:7] == "Bearer " {
				token = token[7:]
			}
		}
		if token == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "missing token"})
			return
		}
		claims, err := s.am.Parse(token)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid token"})
			return
		}
		c.Set("auth_user", &auth.AuthUser{ID: claims.Sub, Role: models.Role(claims.Role)})
		c.Next()
	}
}

func (s *Server) liveStream(c *gin.Context) {
	id := c.Param("id")
	file := c.Param("file")
	root := filepath.Join(s.cfg.LiveDir, id)
	p, ok := s.hls.ResolveFile(root, file)
	if !ok {
		c.Status(http.StatusNotFound)
		return
	}
	if filepath.Ext(p) == ".m3u8" {
		c.Header("Content-Type", "application/vnd.apple.mpegurl")
		c.Header("Cache-Control", "no-cache")
	} else {
		c.Header("Cache-Control", "no-cache")
	}
	c.File(p)
}

func (s *Server) playbackStream(c *gin.Context) {
	session := c.Param("session")
	file := c.Param("file")
	root := filepath.Join(s.cfg.LiveDir, "playback", session)
	p, ok := s.hls.ResolveFile(root, file)
	if !ok {
		c.Status(http.StatusNotFound)
		return
	}
	if filepath.Ext(p) == ".m3u8" {
		c.Header("Content-Type", "application/vnd.apple.mpegurl")
		c.Header("Cache-Control", "no-cache")
	} else {
		c.Header("Cache-Control", "no-cache")
	}
	c.File(p)
}

func removeEventFiles(eventDir, deviceID, eventID string) error {
	return os.RemoveAll(filepath.Join(eventDir, deviceID, eventID))
}
