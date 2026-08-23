package api

import (
	"encoding/json"
	"net/http"
	"os"
	"sync"

	"github.com/gin-gonic/gin"

	"simplenvr/server/auth"
	"simplenvr/server/config"
	"simplenvr/server/pkg/hls"
	"simplenvr/server/pkg/recorder"
	"simplenvr/server/store"
)

type Server struct {
	cfg *config.Config
	st  *store.Store
	rec *recorder.Manager
	hls *hls.Hls
	am  *auth.Manager
	hub *SSEHub

	settingsMu sync.Mutex
	settings   *AppSettings
}

type AIConfig struct {
	Enabled   bool    `json:"enabled"`
	Mode      string  `json:"mode"` // "local" (on-device detect) | "openai" (vision API)
	BaseURL   string  `json:"baseUrl"`
	DetectURL string  `json:"detectUrl"`
	Model     string  `json:"model"`
	ModelPath string  `json:"modelPath"`
	APIKey    string  `json:"apiKey"`
	Prompt    string  `json:"prompt"`
	Interval  int     `json:"interval"`
	Cooldown  int     `json:"cooldown"`
	Threshold float64 `json:"threshold"`
}

type AppSettings struct {
	RetentionDays int       `json:"retentionDays"`
	RecordMode    string    `json:"recordMode"`
	ScheduleStart string    `json:"scheduleStart"`
	ScheduleEnd   string    `json:"scheduleEnd"`
	MotionPush    bool      `json:"motionPush"`
	OfflinePush   bool      `json:"offlinePush"`
	HTTPS         bool      `json:"https"`
	AI            AIConfig  `json:"ai"`
}

func New(cfg *config.Config, st *store.Store, rec *recorder.Manager, h *hls.Hls, am *auth.Manager, hub *SSEHub) *Server {
	s := &Server{cfg: cfg, st: st, rec: rec, hls: h, am: am, hub: hub}
	s.loadSettings()
	s.applySettings()
	s.rec.ShouldRecordFn = func() (string, string, string) {
		s.settingsMu.Lock()
		defer s.settingsMu.Unlock()
		return s.settings.RecordMode, s.settings.ScheduleStart, s.settings.ScheduleEnd
	}
	return s
}

func (s *Server) settingsPath() string { return s.cfg.DataDir + "/settings.json" }

func (s *Server) loadSettings() {
	def := defaultSettings()
	s.settings = &def
	data, err := os.ReadFile(s.settingsPath())
	if err != nil {
		return
	}
	_ = json.Unmarshal(data, s.settings)
}

func defaultSettings() AppSettings {
	return AppSettings{
		RetentionDays: 30,
		RecordMode:    "continuous",
		ScheduleStart: "08:00",
		ScheduleEnd:   "20:00",
		MotionPush:    true,
		OfflinePush:   true,
		HTTPS:         false,
		AI: AIConfig{
			Enabled:   false,
			Mode:      "local",
			BaseURL:   "http://localhost:11434/v1",
			DetectURL: "http://localhost:11435",
			Model:     "person-detection",
			ModelPath: "",
			APIKey:    "",
			Prompt:    "你是安防监控分析助手。分析图中画面，仅输出JSON：{\"alert\":true/false,\"label\":\"事件类别\",\"description\":\"简短中文描述\"}。出现人员、车辆、异常闯入、火焰烟雾等视为 alert=true。",
			Interval:  10,
			Cooldown:  60,
			Threshold: 0.5,
		},
	}
}

func (s *Server) applySettings() {
	s.cfg.AIEnabled = s.settings.AI.Enabled
	s.cfg.AIMode = s.settings.AI.Mode
	s.cfg.AIBaseURL = s.settings.AI.BaseURL
	s.cfg.AIDetectURL = s.settings.AI.DetectURL
	s.cfg.AIModel = s.settings.AI.Model
	s.cfg.AIModelPath = s.settings.AI.ModelPath
	s.cfg.AIModelPath = s.settings.AI.ModelPath
	s.cfg.AIAPIKey = s.settings.AI.APIKey
	s.cfg.AIPrompt = s.settings.AI.Prompt
	if s.settings.AI.Interval > 0 {
		s.cfg.SnapshotIntervalSec = s.settings.AI.Interval
	}
	if s.settings.AI.Cooldown > 0 {
		s.cfg.AICooldown = s.settings.AI.Cooldown
	}
	s.cfg.AIThreshold = s.settings.AI.Threshold
	if s.settings.RetentionDays > 0 {
		s.cfg.RetentionDays = s.settings.RetentionDays
	}
}

func (s *Server) saveSettings() {
	s.settingsMu.Lock()
	data, _ := json.MarshalIndent(s.settings, "", "  ")
	s.settingsMu.Unlock()
	_ = os.WriteFile(s.settingsPath(), data, 0o644)
}

func (s *Server) Router() http.Handler {
	r := gin.New()
	r.Use(gin.Logger(), gin.Recovery())

	api := r.Group("/api")
	api.GET("/health", s.health)
	api.GET("/events/sse", s.sseHandler)

	authGrp := api.Group("/auth")
	authGrp.POST("/login", s.login)

	protected := api.Group("")
	protected.Use(s.am.Middleware())
	protected.GET("/auth/me", s.me)
	protected.GET("/users", s.requireAdmin, s.listUsers)
	protected.POST("/users", s.requireAdmin, s.createUser)
	protected.PUT("/users/:id", s.requireAdmin, s.updateUser)
	protected.DELETE("/users/:id", s.requireAdmin, s.deleteUser)
	protected.PUT("/users/me/password", s.changeOwnPassword)

	protected.GET("/devices", s.listDevices)
	protected.POST("/devices", s.requireOperator, s.createDevice)
	protected.PUT("/devices/:id", s.requireOperator, s.updateDevice)
	protected.DELETE("/devices/:id", s.requireAdmin, s.deleteDevice)
	protected.POST("/devices/test", s.requireOperator, s.testDevice)
	protected.POST("/devices/streams", s.requireOperator, s.probeStreams)
	protected.POST("/devices/discover", s.requireOperator, s.discoverDevices)
	protected.POST("/devices/:id/probe", s.requireOperator, s.probeDevice)
	protected.GET("/devices/:id/snapshot", s.deviceSnapshot)
	protected.GET("/devices/:id/recordings", s.deviceRecordings)
	protected.GET("/devices/:id/recordings/:date/:time/download", s.downloadRecording)
	protected.GET("/devices/:id/month", s.deviceMonth)
	protected.POST("/devices/:id/playback", s.createPlayback)

	protected.GET("/events", s.listEvents)
	protected.GET("/events/:id/snapshot", s.eventSnapshot)
	protected.GET("/events/:id/gif", s.eventGIF)
	protected.DELETE("/events/:id", s.requireOperator, s.deleteEvent)
	protected.GET("/events/:id/snapshot/download", s.downloadEventSnapshot)
	protected.GET("/events/:id/gif/download", s.downloadEventGIF)

	protected.GET("/settings", s.getSettings)
	protected.PUT("/settings", s.requireAdmin, s.putSettings)
	protected.GET("/storage", s.storageInfo)
	protected.GET("/ai/models", s.requireOperator, s.listAIModels)
	protected.POST("/ai/load", s.requireOperator, s.loadAIModel)

	stream := r.Group("/api/stream")
	stream.Use(s.streamAuth())
	stream.GET("/live/:id/*file", s.liveStream)
	stream.GET("/playback/:session/*file", s.playbackStream)

	if s.cfg.WebDir != "" {
		if _, err := os.Stat(s.cfg.WebDir + "/index.html"); err == nil {
			r.Static("/assets", s.cfg.WebDir+"/assets")
			r.StaticFile("/manifest.webmanifest", s.cfg.WebDir+"/manifest.webmanifest")
			r.StaticFile("/manifest.json", s.cfg.WebDir+"/manifest.json")
			r.StaticFile("/icon.svg", s.cfg.WebDir+"/icon.svg")
			r.StaticFile("/icon-192.png", s.cfg.WebDir+"/icon-192.png")
			r.StaticFile("/icon-512.png", s.cfg.WebDir+"/icon-512.png")
			r.StaticFile("/icon-maskable-512.png", s.cfg.WebDir+"/icon-maskable-512.png")
			r.StaticFile("/registerSW.js", s.cfg.WebDir+"/registerSW.js")
			r.StaticFile("/sw.js", s.cfg.WebDir+"/sw.js")
			r.NoRoute(func(c *gin.Context) {
				if c.Request.Method == http.MethodGet {
					c.File(s.cfg.WebDir + "/index.html")
					return
				}
				c.Status(http.StatusNotFound)
			})
		}
	}
	return r
}
