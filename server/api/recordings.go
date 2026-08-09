package api

import (
	"net/http"
	"path/filepath"
	"time"

	"github.com/gin-gonic/gin"
)

func (s *Server) downloadRecording(c *gin.Context) {
	deviceID := c.Param("deviceId")
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
	if len(timeStr) < 4 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "bad time"})
		return
	}
	fileName := timeStr[:4] + ".mp4"
	dirPath := filepath.Join(s.cfg.RecordDir, deviceID, day.Format("20060102"))
	fullPath := filepath.Join(dirPath, fileName)
	if !fileExists(fullPath) {
		c.JSON(http.StatusNotFound, gin.H{"error": "recording not found"})
		return
	}
	c.Header("Content-Disposition", "attachment; filename="+dateStr+"_"+timeStr+".mp4")
	c.File(fullPath)
}

func (s *Server) downloadEventSnapshot(c *gin.Context) {
	id := c.Param("id")
	deviceID := c.Param("deviceId")
	p := filepath.Join(s.cfg.EventDir, deviceID, id, "snapshot.jpg")
	if !fileExists(p) {
		c.JSON(http.StatusNotFound, gin.H{"error": "snapshot not found"})
		return
	}
	c.Header("Content-Disposition", "attachment; filename="+id+".jpg")
	c.File(p)
}

func (s *Server) downloadEventGIF(c *gin.Context) {
	id := c.Param("id")
	deviceID := c.Param("deviceId")
	p := filepath.Join(s.cfg.EventDir, deviceID, id, "animation.gif")
	if !fileExists(p) {
		c.JSON(http.StatusNotFound, gin.H{"error": "gif not found"})
		return
	}
	c.Header("Content-Disposition", "attachment; filename="+id+".gif")
	c.File(p)
}
