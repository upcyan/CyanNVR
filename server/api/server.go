package api

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"

	"cyannvr/server/auth"
	"cyannvr/server/config"
	"cyannvr/server/pkg/hls"
	"cyannvr/server/pkg/recorder"
	"cyannvr/server/pkg/tlsx"
	"cyannvr/server/store"
)

type Server struct {
	cfg *config.Config
	st  *store.Store
	rec *recorder.Manager
	hls *hls.Hls
	am  *auth.Manager
	hub *SSEHub
	tls *tlsx.Runner

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
	RetentionDays int      `json:"retentionDays"`
	RecordMode    string   `json:"recordMode"`
	ScheduleStart string   `json:"scheduleStart"`
	ScheduleEnd   string   `json:"scheduleEnd"`
	MotionPush    bool     `json:"motionPush"`
	OfflinePush   bool     `json:"offlinePush"`
	HTTPS         bool     `json:"https"`
	HTTPSPort     int      `json:"httpsPort"`
	TLSCertMode   string   `json:"tlsCertMode"` // "auto"(ACME) | "manual"
	TLSDomain     string   `json:"tlsDomain"`
	ACMEEmail     string   `json:"acmeEmail"`
	AI            AIConfig `json:"ai"`
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

	// 配置迁移：早期版本把内置检测进程地址写作 TCP(http://localhost:11435)，
	// 该端口现已不再监听（改为 Unix Domain Socket）。
	// 此处自动迁移，否则升级后旧配置会覆盖环境变量默认值，导致 AI 接口 502。
	if isLegacyDetectURL(s.settings.AI.DetectURL) {
		target := s.cfg.AIDetectURL
		if target == "" {
			target = "unix:/tmp/cyannvr-ai.sock"
		}
		log.Printf("migrating AI detect url %q -> %q", s.settings.AI.DetectURL, target)
		s.settings.AI.DetectURL = target
		s.saveSettings()
	}
}

// isLegacyDetectURL 判断是否为旧版本遗留的本地 TCP 检测地址。
func isLegacyDetectURL(u string) bool {
	switch strings.TrimRight(u, "/") {
	case "http://localhost:11435", "http://127.0.0.1:11435", "":
		return true
	}
	return false
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
		HTTPSPort:     443,
		TLSCertMode:   "",
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
	s.settingsMu.Lock()
	set := *s.settings
	s.settingsMu.Unlock()
	s.cfg.AIEnabled = set.AI.Enabled
	s.cfg.AIMode = set.AI.Mode
	s.cfg.AIBaseURL = set.AI.BaseURL
	s.cfg.AIDetectURL = set.AI.DetectURL
	s.cfg.AIModel = set.AI.Model
	s.cfg.AIModelPath = set.AI.ModelPath
	s.cfg.AIAPIKey = set.AI.APIKey
	s.cfg.AIPrompt = set.AI.Prompt
	if set.AI.Interval > 0 {
		s.cfg.SnapshotIntervalSec = set.AI.Interval
	}
	if set.AI.Cooldown > 0 {
		s.cfg.AICooldown = set.AI.Cooldown
	}
	s.cfg.AIThreshold = set.AI.Threshold
	if set.RetentionDays > 0 {
		s.cfg.RetentionDays = set.RetentionDays
	}
}

func (s *Server) saveSettings() {
	s.settingsMu.Lock()
	data, _ := json.MarshalIndent(s.settings, "", "  ")
	s.settingsMu.Unlock()
	_ = os.WriteFile(s.settingsPath(), data, 0o600)
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
	protected.GET("/tls/status", s.tlsStatus)
	protected.POST("/tls/manual-cert", s.requireAdmin, s.uploadManualCert)
	protected.POST("/tls/reload", s.requireAdmin, s.reloadTLS)
	protected.GET("/storage", s.storageInfo)
	protected.GET("/ai/models", s.requireOperator, s.listAIModels)
	protected.POST("/ai/load", s.requireOperator, s.loadAIModel)
	// 后端状态只读，所有登录用户都能看到「当前用的是 CPU 还是 GPU」
	protected.GET("/ai/status", s.aiWorkerStatus)
	protected.POST("/ai/download", s.requireOperator, s.downloadAIModel)

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
