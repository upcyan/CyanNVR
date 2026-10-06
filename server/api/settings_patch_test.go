package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"cyannvr/server/config"
	"github.com/gin-gonic/gin"
)

func settingsTestServer(t *testing.T) (*Server, *gin.Engine) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	settings := defaultSettings()
	settings.AI.Enabled = true
	settings.AI.ModelPath = "custom-model.onnx"
	s := &Server{cfg: &config.Config{DataDir: t.TempDir()}, settings: &settings}
	r := gin.New()
	r.PATCH("/settings", s.putSettings)
	r.PUT("/settings", s.putSettings)
	r.GET("/settings", s.getSettings)
	return s, r
}
func settingsRequest(r http.Handler, method, body, etag string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, "/settings", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	if etag != "" {
		req.Header.Set("If-Match", etag)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}
func TestSettingsPatchPreservesOtherFields(t *testing.T) {
	s, r := settingsTestServer(t)
	for _, body := range []string{`{"ai":{"enabled":false}}`, `{"motionPush":false}`, `{"ai":{"threshold":0.7}}`} {
		w := settingsRequest(r, "PATCH", body, "")
		if w.Code != 200 {
			t.Fatalf("%d %s", w.Code, w.Body.String())
		}
	}
	if s.settings.AI.Enabled || s.settings.MotionPush || s.settings.AI.Threshold != 0.7 || s.settings.AI.ModelPath != "custom-model.onnx" {
		t.Fatal("leaf merge lost fields")
	}
	b, e := os.ReadFile(filepath.Join(s.cfg.DataDir, "settings.json"))
	if e != nil {
		t.Fatal(e)
	}
	var saved AppSettings
	if e = json.Unmarshal(b, &saved); e != nil {
		t.Fatal(e)
	}
	if saved.AI != s.settings.AI {
		t.Fatal("disk and memory diverged")
	}
}
func TestSettingsConcurrentPatches(t *testing.T) {
	s, r := settingsTestServer(t)
	var wg sync.WaitGroup
	for _, body := range []string{`{"ai":{"enabled":false}}`, `{"motionPush":false}`} {
		wg.Add(1)
		go func(b string) {
			defer wg.Done()
			w := settingsRequest(r, "PATCH", b, "")
			if w.Code != 200 {
				t.Errorf("%d %s", w.Code, w.Body.String())
			}
		}(body)
	}
	wg.Wait()
	if s.settings.AI.Enabled || s.settings.MotionPush {
		t.Fatal("concurrent updates overwrote each other")
	}
}
func TestSettingsRejectsLegacyAndStaleReplacement(t *testing.T) {
	s, r := settingsTestServer(t)
	b, _ := json.Marshal(s.settings)
	if w := settingsRequest(r, "PUT", string(b), ""); w.Code != 428 {
		t.Fatalf("old client accepted: %d", w.Code)
	}
	etag := settingsETag(*s.settings)
	if w := settingsRequest(r, "PATCH", `{"offlinePush":false}`, ""); w.Code != 200 {
		t.Fatal(w.Code)
	}
	if w := settingsRequest(r, "PUT", string(b), etag); w.Code != 412 {
		t.Fatalf("stale replacement accepted: %d", w.Code)
	}
	if w := settingsRequest(r, "PUT", string(b), settingsETag(*s.settings)); w.Code != 200 {
		t.Fatalf("valid replacement: %d %s", w.Code, w.Body.String())
	}
}
func TestSettingsRejectsInvalidPatch(t *testing.T) {
	s, r := settingsTestServer(t)
	original := settingsETag(*s.settings)
	for _, body := range []string{`null`, `{"ai":null}`, `{"ai":{"enabled":"false"}}`, `{"ai":{"unknown":1}}`, `{"retentionDays":0}`, `{"unknown":1}`} {
		if w := settingsRequest(r, "PATCH", body, ""); w.Code != 400 {
			t.Errorf("%s accepted: %d", body, w.Code)
		}
		if settingsETag(*s.settings) != original {
			t.Fatal("invalid patch changed memory")
		}
	}
}
func TestSettingsWriteFailureDoesNotPublish(t *testing.T) {
	s, r := settingsTestServer(t)
	s.cfg.DataDir = filepath.Join(s.cfg.DataDir, "nonexistent")
	if w := settingsRequest(r, "PATCH", `{"ai":{"enabled":false}}`, ""); w.Code != 500 {
		t.Fatal(w.Code)
	}
	if !s.settings.AI.Enabled {
		t.Fatal("failed write published state")
	}
}
