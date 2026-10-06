package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"

	"cyannvr/server/auth"
	"cyannvr/server/config"
	"cyannvr/server/models"
	"cyannvr/server/store"
)

// newRobustnessServer 构造带真实空库与鉴权管理器的最小 Server，
// 供「App 客户端健壮性」回归测试使用。
func newRobustnessServer(t *testing.T) (*Server, *auth.Manager) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	am := auth.NewManager("robustness-test-secret-0123456789")
	cfg := &config.Config{DataDir: t.TempDir(), MinFreeMB: 512}
	return &Server{cfg: cfg, st: st, am: am}, am
}

func doJSON(t *testing.T, r http.Handler, method, target, token string) (int, string, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(method, target, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	body := w.Body.String()
	var m map[string]any
	_ = json.Unmarshal([]byte(body), &m)
	return w.Code, body, m
}

// assertListNeverNull 是 App 健壮性的核心断言：所有列表字段必须是 []，
// 绝不能是 null（严格 JSON 客户端对 null 做集合操作会崩溃）。
func assertListNeverNull(t *testing.T, m map[string]any, keys ...string) {
	t.Helper()
	for _, k := range keys {
		v, ok := m[k]
		if !ok {
			t.Errorf("响应缺少字段 %q", k)
			continue
		}
		if v == nil {
			t.Errorf("字段 %q 为 null，必须返回空数组 []", k)
		}
	}
}

func testAdminUser() models.User {
	return models.User{
		ID:       "u_app",
		Username: "apptest",
		Role:     models.RoleAdmin,
	}
}

// TestAppEmptyDBListsNeverNull 空库下所有列表端点必须返回 [] 而非 null，
// 未匹配的 /api/* 路由必须返回 JSON 404（App 按 JSON 解析响应，
// SPA index.html(200+HTML) 或空体 404 都会让其解析崩溃且无从判断错误）。
func TestAppEmptyDBListsNeverNull(t *testing.T) {
	s, am := newRobustnessServer(t)
	r := s.Router()

	u := testAdminUser()
	// 真实设备：录像分段端点对「不存在的设备」正确返回 404，
	// 对「存在但无录像」返回 200+[]（本断言的目标场景）
	if err := s.st.CreateDevice(models.Device{ID: "d1", Name: "cam", Source: models.SourceRTSP, Created: time.Now()}); err != nil {
		t.Fatalf("seed device: %v", err)
	}
	pw, err := am.HashPassword("test-password")
	if err != nil {
		t.Fatal(err)
	}
	u.PasswordHash = pw
	u.CreatedAt = time.Now()
	if err := s.st.CreateUser(u); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	tok, err := am.Issue(&u)
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name, method, target string
		listKeys             []string
	}{
		{"设备列表", "GET", "/api/devices", []string{"devices"}},
		{"用户列表", "GET", "/api/users", []string{"users"}},
		{"事件列表", "GET", "/api/events", []string{"events"}},
		{"录像分段(空日)", "GET", "/api/devices/d1/recordings?date=2030-01-01", []string{"segments"}},
		{"月历", "GET", "/api/devices/d1/month?ym=2030-01", []string{"days"}},
		{"品牌模板", "GET", "/api/devices/brands", []string{"brands"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			code, _, m := doJSON(t, r, c.method, c.target, tok)
			if code != http.StatusOK {
				t.Fatalf("状态码 %d，期望 200（空结果是正常业务态，不应报错）", code)
			}
			assertListNeverNull(t, m, c.listKeys...)
		})
	}

	// 未匹配的 /api/* 路由：必须 JSON 404，绝不能回 SPA index.html（200+HTML）
	for _, method := range []string{"GET", "POST"} {
		code, body, m := doJSON(t, r, method, "/api/definitely/not/exist", tok)
		if code != http.StatusNotFound {
			t.Fatalf("未知 /api 路由(%s)状态码 %d，期望 404", method, code)
		}
		if strings.Contains(body, "<html") || strings.Contains(body, "<!DOCTYPE") {
			t.Fatalf("未知 /api 路由(%s)返回了 HTML，App 的 JSON 解析会崩溃", method)
		}
		if m["error"] == nil {
			t.Fatalf("未知 /api 路由(%s)响应缺少 error 字段", method)
		}
	}
}

// craftToken 直接构造指定 exp/tw 的 token（Manager.Issue 只能签「未过期」token）。
func craftToken(secret string, sub, role string, exp time.Time, tw *time.Time) (string, error) {
	claims := auth.Claims{
		Sub:  sub,
		Role: role,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(exp),
			IssuedAt:  jwt.NewNumericDate(exp.Add(-24 * time.Hour)),
			Subject:   sub,
		},
	}
	if tw != nil {
		claims.TrustUntil = jwt.NewNumericDate(*tw)
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(secret))
}

// TestStreamAuthTrustWindow 流媒体 query token 在「已过期但仍在免登录信任窗口」
// 内必须放行：App 只持 query token 拉流、拿不到 X-Renewed-Token 续签响应头，
// 严格校验会让视频流在 token 每日过期时断流一次（web 端感知不到此问题，
// 因为浏览器每次 API 调用都会自动换新 token）。
func TestStreamAuthTrustWindow(t *testing.T) {
	s, am := newRobustnessServer(t)
	// 与 main.go 一致：接上「改密时间查询」回调，改密即吊销旧 token
	am.SetRevokedAfter(func(userID string) time.Time {
		tm, err := s.st.PasswordChangedAt(userID)
		if err != nil {
			return time.Time{}
		}
		return tm
	})
	r := s.Router()
	const secret = "robustness-test-secret-0123456789"

	u := testAdminUser()
	if err := s.st.CreateUser(u); err != nil {
		t.Fatal(err)
	}

	tw := time.Now().Add(72 * time.Hour)
	cases := []struct {
		name   string
		exp    time.Time
		tw     *time.Time
		expect string // "pass" | "401"
	}{
		{"有效token", time.Now().Add(time.Hour), nil, "pass"},
		{"过期但在信任窗口", time.Now().Add(-time.Hour), &tw, "pass"},
		{"过期且无信任窗口", time.Now().Add(-time.Hour), nil, "401"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tok, err := craftToken(secret, u.ID, string(models.RoleAdmin), c.exp, c.tw)
			if err != nil {
				t.Fatal(err)
			}
			// 目标设备不存在：鉴权通过后走业务 404；被鉴权拦截则是 401
			code, _, _ := doJSON(t, r, "GET", "/api/stream/live/d1/index.m3u8?token="+tok, "")
			switch c.expect {
			case "pass":
				if code == http.StatusUnauthorized {
					t.Fatalf("状态码 401：信任窗口内的过期 token 被拒绝，App 视频流会每日断流")
				}
			case "401":
				if code != http.StatusUnauthorized {
					t.Fatalf("状态码 %d，期望 401", code)
				}
			}
		})
	}

	// 改密吊销优先于信任窗口：旧 token 即使在窗口内也必须拒绝
	tw2 := time.Now().Add(72 * time.Hour)
	tok, err := craftToken(secret, u.ID, string(models.RoleAdmin), time.Now().Add(-time.Hour), &tw2)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.st.UpdateUserPassword(u.ID, "newhash"); err != nil {
		t.Fatal(err)
	}
	if code, _, _ := doJSON(t, r, "GET", "/api/stream/live/d1/index.m3u8?token="+tok, ""); code != http.StatusUnauthorized {
		t.Fatalf("改密后旧 token 状态码 %d，期望 401", code)
	}
}
