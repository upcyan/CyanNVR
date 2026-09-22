package main

import (
	"crypto/rand"
	"encoding/base64"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"cyannvr/server/api"
	"cyannvr/server/auth"
	"cyannvr/server/config"
	"cyannvr/server/models"
	"cyannvr/server/pkg/ai"
	"cyannvr/server/pkg/ffmpeg"
	"cyannvr/server/pkg/hls"
	"cyannvr/server/pkg/recorder"
	"cyannvr/server/pkg/tlsx"
	"cyannvr/server/store"
)

func main() {
	// 子命令分发：reset-password / list-users 等维护命令直接执行后退出，
	// 不进入服务启动流程（见 cli.go）。无参数时按原行为启动服务。
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "reset-password", "reset", "list-users", "users", "rename-user", "rename", "help", "-h", "--help":
			runCLI(os.Args[1:])
			return
		}
	}

	// 把核心版本暴露给 api 包，健康检查里会返回，便于确认运行的是哪个版本
	api.CoreVersion = CoreVersion

	cfg := config.Load()
	if cfg.JWTSecret == "cyannvr-dev-secret-change-me" {
		log.Printf("WARNING: using default JWT secret; set NVR_JWT_SECRET in production")
	}

	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		log.Fatalf("mkdir data dir: %v", err)
	}

	jwtSecret, err := loadOrCreateJWTSecret(cfg)
	if err != nil {
		log.Fatalf("load jwt secret: %v", err)
	}
	cfg.JWTSecret = jwtSecret

	st, err := store.Open(filepath.Join(cfg.DataDir, "nvr.db"))
	if err != nil {
		log.Fatalf("open store: %v", err)
	}
	defer st.Close()

	seedAdmin(cfg, st)

	am := auth.NewManager(cfg.JWTSecret)
	// 改密即注销旧会话：鉴权中间件会拿该用户最后一次改密时间，
	// 拒绝早于该时刻签发的 token（重置密码 / 修改密码后旧 token 立刻失效）。
	//
	// store 使用 MaxOpenConns(1)，查询串行化；而 HLS 分片、SSE 等请求都会
	// 触发鉴权。这里加一层 5 秒 TTL 缓存：改密最迟 5 秒后生效，但把每请求
	// 一次查库降到几乎为零，避免鉴权与录像写入抢同一个连接。
	revokeCache := newRevokeCache(5 * time.Second)
	am.SetRevokedAfter(func(userID string) time.Time {
		if t, ok := revokeCache.get(userID); ok {
			return t
		}
		t, err := st.PasswordChangedAt(userID)
		if err != nil {
			// 查询失败时保守放行，避免数据库抖动把所有人踢下线；
			// token 自身的过期时间仍然有效。
			return time.Time{}
		}
		revokeCache.set(userID, t)
		return t
	})
	hub := api.NewSSEHub()

	broadcast := func(eventType, deviceID, deviceName, eventID, label, desc, t string) {
		hub.Broadcast(api.Notification{
			Type:       eventType,
			DeviceID:   deviceID,
			DeviceName: deviceName,
			EventID:    eventID,
			Label:      label,
			Desc:       desc,
			Time:       t,
		})
	}

	analyzer := ai.New(cfg, st, broadcast)
	ai.StartLocalWorker(cfg.AIDetectURL)
	rec := recorder.NewManager(cfg, st, analyzer, broadcast)
	hlsSvc := hls.New(cfg, st)

	server := api.New(cfg, st, rec, hlsSvc, am, hub)
	rec.Start()
	defer rec.Stop()

	router := server.Router()
	tlsRunner := tlsx.New(cfg.DataDir, router)
	server.SetTLSRunner(tlsRunner)
	go tlsRunner.Apply(server.CurrentTLSConfig())

	addr := ":" + cfg.Port
	log.Printf("CyanNVR listening on %s", addr)
	log.Printf("ffmpeg available: %v", ffmpeg.Exists(cfg.Ffmpeg))
	go startMDNS(cfg.Port)
	if cfg.WebDir != "" {
		if _, err := os.Stat(filepath.Join(cfg.WebDir, "index.html")); err == nil {
			log.Printf("serving web UI from %s", cfg.WebDir)
		}
	}
	srv := &http.Server{
		Addr:              addr,
		Handler:           router,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      120 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}
	if err := srv.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}

func seedAdmin(cfg *config.Config, st *store.Store) {
	// 管理员用户名可通过 NVR_ADMIN_USER 定制（安装向导中配置），默认 admin。
	// 仅首次初始化时生效：一旦该用户已存在就不再改动。
	name := strings.TrimSpace(os.Getenv("NVR_ADMIN_USER"))
	if name == "" {
		name = "admin"
	}
	if len(name) > 32 {
		name = name[:32]
	}
	existing, err := st.GetUserByName(name)
	if err != nil {
		log.Printf("seed admin check: %v", err)
		return
	}
	if existing != nil {
		return
	}
	// 数据库非空时完全跳过初始化：NVR_ADMIN_USER / NVR_ADMIN_PASSWORD 只在
	// 首次建库时生效。否则向导里残留的旧用户名（例如自定义了 cnvradmin 但
	// 库里实际是 admin）会每次启动都尝试插入，撞上固定主键 u_admin，
	// 日志里永远刷 UNIQUE constraint failed: users.id。
	all, lerr := st.ListUsers()
	if lerr == nil && len(all) > 0 {
		log.Printf("seed admin: 数据库已有 %d 个用户，忽略 NVR_ADMIN_USER=%q（改名/改密请用 cyannvr reset-password 或网页「忘记密码」）",
			len(all), name)
		return
	}
	pw := os.Getenv("NVR_ADMIN_PASSWORD")
	if pw == "" {
		pw = "admin123"
	}
	am := auth.NewManager(cfg.JWTSecret)
	hash, err := am.HashPassword(pw)
	if err != nil {
		log.Printf("hash admin password: %v", err)
		return
	}
	u := models.User{
		ID:           "u_admin",
		Username:     name,
		PasswordHash: hash,
		Role:         models.RoleAdmin,
		CreatedAt:    time.Now(),
	}
	if err := st.CreateUser(u); err != nil {
		log.Printf("seed admin: %v", err)
		return
	}
	log.Printf("seeded admin user %q (password set via NVR_ADMIN_PASSWORD)", name)
}

// revokeCache 缓存「用户最后一次改密时间」，避免每个鉴权请求都查库。
// 代价是改密后最迟 TTL 才生效；TTL 设得很短（5 秒），实际感知为即时下线。
type revokeCache struct {
	mu  sync.Mutex
	ttl time.Duration
	m   map[string]revokeEntry
}

type revokeEntry struct {
	at  time.Time
	exp time.Time
}

func newRevokeCache(ttl time.Duration) *revokeCache {
	return &revokeCache{ttl: ttl, m: make(map[string]revokeEntry)}
}

func (c *revokeCache) get(userID string) (time.Time, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.m[userID]
	if !ok || time.Now().After(e.exp) {
		return time.Time{}, false
	}
	return e.at, true
}

func (c *revokeCache) set(userID string, at time.Time) {
	now := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()
	// 用户数量极少，简单清理即可，避免长期运行后 map 无限增长
	if len(c.m) > 256 {
		for k, e := range c.m {
			if now.After(e.exp) {
				delete(c.m, k)
			}
		}
	}
	c.m[userID] = revokeEntry{at: at, exp: now.Add(c.ttl)}
}

// loadOrCreateJWTSecret returns the configured secret, or auto-generates a
// random one persisted to DataDir/jwt_secret so tokens survive restarts while
// never falling back to a guessable default.
func loadOrCreateJWTSecret(cfg *config.Config) (string, error) {
	if cfg.JWTSecret != "" {
		// Reject known weak/default secrets.
		if cfg.JWTSecret == "cyannvr-dev-secret-change-me" || len(cfg.JWTSecret) < 16 {
			log.Fatal("REFUSING weak JWT secret — set NVR_JWT_SECRET to a strong random value (>= 16 chars)")
		}
		return cfg.JWTSecret, nil
	}
	path := filepath.Join(cfg.DataDir, "jwt_secret")
	if data, err := os.ReadFile(path); err == nil && len(strings.TrimSpace(string(data))) >= 32 {
		return strings.TrimSpace(string(data)), nil
	}
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	secret := base64.RawURLEncoding.EncodeToString(buf)
	if err := os.WriteFile(path, []byte(secret), 0o600); err != nil {
		return "", err
	}
	log.Printf("generated new JWT secret at %s", path)
	return secret, nil
}
