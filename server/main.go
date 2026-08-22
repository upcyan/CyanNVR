package main

import (
	"crypto/rand"
	"encoding/base64"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"simplenvr/server/api"
	"simplenvr/server/auth"
	"simplenvr/server/config"
	"simplenvr/server/models"
	"simplenvr/server/pkg/ai"
	"simplenvr/server/pkg/ffmpeg"
	"simplenvr/server/pkg/hls"
	"simplenvr/server/pkg/recorder"
	"simplenvr/server/store"
)

func main() {
	cfg := config.Load()

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

	addr := ":" + cfg.Port
	log.Printf("SimpleNVR listening on %s", addr)
	log.Printf("ffmpeg available: %v", ffmpeg.Exists(cfg.Ffmpeg))
	if cfg.WebDir != "" {
		if _, err := os.Stat(filepath.Join(cfg.WebDir, "index.html")); err == nil {
			log.Printf("serving web UI from %s", cfg.WebDir)
		}
	}
	srv := &http.Server{
		Addr:         addr,
		Handler:      server.Router(),
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 0,
	}
	if err := srv.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}

func seedAdmin(cfg *config.Config, st *store.Store) {
	existing, err := st.GetUserByName("admin")
	if err != nil {
		log.Printf("seed admin check: %v", err)
		return
	}
	if existing != nil {
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
		Username:     "admin",
		PasswordHash: hash,
		Role:         models.RoleAdmin,
		CreatedAt:    time.Now(),
	}
	if err := st.CreateUser(u); err != nil {
		log.Printf("seed admin: %v", err)
		return
	}
	log.Printf("seeded admin user (default password: %s)", pw)
}

// loadOrCreateJWTSecret returns the configured secret, or auto-generates a
// random one persisted to DataDir/jwt_secret so tokens survive restarts while
// never falling back to a guessable default.
func loadOrCreateJWTSecret(cfg *config.Config) (string, error) {
	if cfg.JWTSecret != "" {
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
