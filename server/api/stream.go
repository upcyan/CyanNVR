package api

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"

	"cyannvr/server/auth"
	"cyannvr/server/models"
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
		c.Set("stream_token", token)
		c.Next()
	}
}

// servePlaylist reads an HLS playlist and rewrites every segment URI to an
// absolute URL carrying the stream token, so hls.js/native players can fetch
// segments without losing authentication.
func servePlaylist(c *gin.Context, p string) {
	data, err := os.ReadFile(p)
	if err != nil {
		c.Status(http.StatusNotFound)
		return
	}
	token := c.GetString("stream_token")
	base := c.Request.URL.Path
	if i := strings.LastIndex(base, "/"); i >= 0 {
		base = base[:i+1]
	}
	lines := strings.Split(string(data), "\n")
	out := make([]string, 0, len(lines))
	for _, ln := range lines {
		line := strings.TrimSpace(ln)
		if line == "" || strings.HasPrefix(line, "#") {
			out = append(out, ln)
			continue
		}
		seg := line
		if q := strings.IndexByte(seg, '?'); q >= 0 {
			seg = seg[:q]
		}
		out = append(out, base+seg+"?token="+token)
	}
	c.Header("Content-Type", "application/vnd.apple.mpegurl")
	c.Header("Cache-Control", "no-cache")
	c.Writer.WriteString(strings.Join(out, "\n"))
}

func (s *Server) liveStream(c *gin.Context) {
	id := safePathID(c.Param("id"))
	if id == "" {
		c.Status(http.StatusBadRequest)
		return
	}
	file := c.Param("file")
	root := filepath.Join(s.cfg.LiveDir, id)
	p, ok := s.hls.ResolveFile(root, file)
	if !ok {
		c.Status(http.StatusNotFound)
		return
	}
	switch filepath.Ext(p) {
	case ".m3u8":
		servePlaylist(c, p)
	case ".ts":
		c.Header("Content-Type", "video/MP2T")
		c.Header("Cache-Control", "no-cache")
		c.File(p)
	default:
		c.Header("Content-Type", "application/octet-stream")
		c.File(p)
	}
}

func (s *Server) playbackStream(c *gin.Context) {
	session := safePathID(c.Param("session"))
	if session == "" {
		c.Status(http.StatusBadRequest)
		return
	}
	file := c.Param("file")
	root := filepath.Join(s.cfg.LiveDir, "playback", session)
	p, ok := s.hls.ResolveFile(root, file)
	if !ok {
		c.Status(http.StatusNotFound)
		return
	}
	switch filepath.Ext(p) {
	case ".m3u8":
		servePlaylist(c, p)
	case ".ts":
		c.Header("Content-Type", "video/MP2T")
		c.Header("Cache-Control", "no-cache")
		c.File(p)
	default:
		c.Header("Content-Type", "application/octet-stream")
		c.File(p)
	}
}

func removeEventFiles(eventDir, deviceID, eventID string) error {
	return os.RemoveAll(filepath.Join(eventDir, deviceID, eventID))
}
