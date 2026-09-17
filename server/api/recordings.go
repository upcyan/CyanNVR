package api

import (
	"net/http"
	"path/filepath"
	"time"

	"github.com/gin-gonic/gin"
)

func (s *Server) downloadRecording(c *gin.Context) {
	deviceID := safePathID(c.Param("id"))
	dateStr := c.Param("date")
	timeStr := c.Param("time")
	if deviceID == "" || dateStr == "" || timeStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "deviceId, date, time required"})
		return
	}
	day, err := time.ParseInLocation("2006-01-02", dateStr, time.Local)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "bad date"})
		return
	}
	if len(timeStr) != 6 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "bad time (HHMMSS)"})
		return
	}
	for _, ch := range timeStr {
		if ch < '0' || ch > '9' {
			c.JSON(http.StatusBadRequest, gin.H{"error": "bad time (HHMMSS)"})
			return
		}
	}
	dirPath := filepath.Join(s.cfg.RecordDir, deviceID, day.Format("20060102"))
	fullPath := filepath.Join(dirPath, timeStr+".mp4")
	if !fileExists(fullPath) {
		// fall back to the segment starting within the same minute
		matches, _ := filepath.Glob(filepath.Join(dirPath, timeStr[:4]+"*.mp4"))
		if len(matches) == 0 {
			c.JSON(http.StatusNotFound, gin.H{"error": "recording not found"})
			return
		}
		fullPath = matches[0]
	}
	c.Header("Content-Disposition", "attachment; filename="+dateStr+"_"+timeStr+".mp4")
	c.File(fullPath)
}

func (s *Server) downloadEventSnapshot(c *gin.Context) {
	e, ok := s.getEvent(c)
	if !ok {
		return
	}
	p := filepath.Join(s.cfg.EventDir, e.DeviceID, e.ID, "snapshot.jpg")
	if !fileExists(p) {
		c.JSON(http.StatusNotFound, gin.H{"error": "snapshot not found"})
		return
	}
	c.Header("Content-Disposition", "attachment; filename="+e.ID+".jpg")
	c.File(p)
}

func (s *Server) downloadEventGIF(c *gin.Context) {
	e, ok := s.getEvent(c)
	if !ok {
		return
	}
	p := filepath.Join(s.cfg.EventDir, e.DeviceID, e.ID, "animation.gif")
	if !fileExists(p) {
		c.JSON(http.StatusNotFound, gin.H{"error": "gif not found"})
		return
	}
	c.Header("Content-Disposition", "attachment; filename="+e.ID+".gif")
	c.File(p)
}
