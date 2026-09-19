package config

import "os"

type Config struct {
	Port      string
	DataDir   string
	RecordDir string
	LiveDir   string
	SnapDir   string
	EventDir  string
	WebDir    string
	Ffmpeg    string
	JWTSecret string

	RetentionDays       int
	SnapshotIntervalSec int

	AIEnabled   bool
	AIMode      string // "local" | "openai"
	AIBaseURL   string
	AIDetectURL string
	AIModel     string
	AIModelPath string
	AIAPIKey    string
	AIPrompt    string
	AIMinSecs   int
	AICooldown  int
	AIThreshold float64
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func Load() *Config {
	dataDir := env("NVR_DATA", "./data")
	return &Config{
		Port:                env("NVR_PORT", "8080"),
		DataDir:             dataDir,
		RecordDir:           env("NVR_RECORDS", dataDir+"/recordings"),
		LiveDir:             env("NVR_LIVE", dataDir+"/live"),
		SnapDir:             env("NVR_SNAPS", dataDir+"/snapshots"),
		EventDir:            env("NVR_EVENTS", dataDir+"/events"),
		WebDir:              env("NVR_WEB", "./dist"),
		Ffmpeg:              env("FFMPEG", "ffmpeg"),
		JWTSecret:           env("NVR_JWT_SECRET", ""),
		RetentionDays:       30,
		SnapshotIntervalSec: 10,

		AIEnabled:   env("NVR_AI_ENABLED", "false") == "true",
		AIMode:      env("NVR_AI_MODE", "local"),
		AIBaseURL:   env("NVR_AI_BASE_URL", "http://localhost:11434/v1"),
		// 默认使用 Unix Domain Socket 与内置检测进程通信：
		// 不经网络协议栈、不占端口、不对外暴露（此前默认绑 0.0.0.0 存在暴露风险）
		AIDetectURL: env("NVR_AI_DETECT_URL", "unix:/tmp/simplenvr-ai.sock"),
		AIModel:     env("NVR_AI_MODEL", "person-detection"),
		AIModelPath: env("NVR_AI_MODEL_PATH", ""),
		AIAPIKey:    env("NVR_AI_API_KEY", ""),
		AIPrompt:    env("NVR_AI_PROMPT", "你是安防监控分析助手。分析图中画面，仅输出JSON：{\"alert\":true/false,\"label\":\"事件类别\",\"description\":\"简短中文描述\"}。出现人员、车辆、异常闯入、火焰烟雾等视为 alert=true。"),
		AIMinSecs:   15,
		AICooldown:  60,
		AIThreshold: 0.5,
	}
}
