package api

import (
	"net/http"
	"path/filepath"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"cyannvr/server/models"
)

func (s *Server) listEvents(c *gin.Context) {
	deviceID := c.Query("deviceId")
	eventType := c.Query("type")
	dateStr := c.Query("date")
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "100"))
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))
	var dayStart, dayEnd *time.Time
	if dateStr != "" {
		day, err := time.ParseInLocation("2006-01-02", dateStr, time.Local)
		if err == nil {
			ds := day
			de := day.AddDate(0, 0, 1)
			dayStart, dayEnd = &ds, &de
		}
	}
	total, err := s.st.CountEvents(deviceID, eventType, dayStart, dayEnd)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	events, err := s.st.ListEvents(deviceID, eventType, dayStart, dayEnd, limit, offset)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"events": events, "total": total})
}

func (s *Server) eventSnapshot(c *gin.Context) {
	e, ok := s.getEvent(c)
	if !ok {
		return
	}
	if e.Snapshot == "" {
		c.JSON(http.StatusNotFound, gin.H{"error": "no snapshot"})
		return
	}
	s.serveEventFile(c, e.DeviceID, c.Param("id"), "snapshot.jpg")
}

func (s *Server) eventGIF(c *gin.Context) {
	e, ok := s.getEvent(c)
	if !ok {
		return
	}
	if e.GIF == "" {
		c.JSON(http.StatusNotFound, gin.H{"error": "no gif"})
		return
	}
	s.serveEventFile(c, e.DeviceID, c.Param("id"), "animation.gif")
}

func (s *Server) getEvent(c *gin.Context) (*models.Event, bool) {
	e, err := s.st.GetEvent(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return nil, false
	}
	if e == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "event not found"})
		return nil, false
	}
	return e, true
}

func (s *Server) serveEventFile(c *gin.Context, deviceID, eventID, file string) {
	p := filepath.Join(s.cfg.EventDir, deviceID, eventID, file)
	if !fileExists(p) {
		c.JSON(http.StatusNotFound, gin.H{"error": "file not found"})
		return
	}
	c.File(p)
}

func (s *Server) deleteEvent(c *gin.Context) {
	id := c.Param("id")
	e, ok := s.getEvent(c)
	if !ok {
		return
	}
	if err := removeEventFiles(s.cfg.EventDir, e.DeviceID, id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	// simple delete: remove from db by listing all and filtering is heavy;
	// implement direct delete via store extension below.
	if err := s.st.DeleteEvent(id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}
