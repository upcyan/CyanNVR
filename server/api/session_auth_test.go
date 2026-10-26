package api

import (
	"context"
	"cyannvr/server/auth"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestAPIStreamSSESessionPolicy(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name       string
		exp, trust time.Duration
		revoked    bool
		want       int
	}{
		{"valid", time.Hour, 0, false, 200},
		{"expired", -time.Hour, 0, false, 401},
		{"trust_ended", -time.Hour, -time.Minute, false, 401},
		{"trusted", -time.Hour, time.Hour, false, 200},
		{"password_changed", time.Hour, time.Hour, true, 401},
	} {
		t.Run(tc.name, func(t *testing.T) {
			const secret = "shared-test-secret"
			am := auth.NewManager(secret)
			c := auth.Claims{Sub: "u", Role: "admin", RegisteredClaims: jwt.RegisteredClaims{IssuedAt: jwt.NewNumericDate(time.Now().Add(-2 * time.Hour)), ExpiresAt: jwt.NewNumericDate(time.Now().Add(tc.exp))}}
			if tc.trust != 0 {
				c.TrustUntil = jwt.NewNumericDate(time.Now().Add(tc.trust))
			}
			token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, c).SignedString([]byte(secret))
			if err != nil {
				t.Fatal(err)
			}
			if tc.revoked {
				am.SetRevokedAfter(func(string) time.Time { return time.Now() })
			}
			s := &Server{am: am, hub: NewSSEHub()}
			r := gin.New()
			r.GET("/api", am.Middleware(), func(c *gin.Context) { c.Status(200) })
			r.GET("/stream", s.streamAuth(), func(c *gin.Context) { c.Status(200) })
			r.GET("/sse", s.sseHandler)
			for _, path := range []string{"/api", "/stream", "/sse"} {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				req := httptest.NewRequest(http.MethodGet, path+"?token="+token, nil).WithContext(ctx)
				w := httptest.NewRecorder()
				r.ServeHTTP(w, req)
				if w.Code != tc.want {
					t.Errorf("%s status %d want %d", path, w.Code, tc.want)
				}
			}
			if len(s.hub.clients) != 0 {
				t.Fatal("cancelled SSE leaked subscription")
			}
		})
	}
}
