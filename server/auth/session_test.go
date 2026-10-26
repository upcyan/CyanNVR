package auth

import (
	"cyannvr/server/models"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestSessionExpiryAndTrustConsistency(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name    string
		ttl     time.Duration
		trust   time.Duration
		revoked bool
		want    int
	}{
		{"valid", time.Hour, 0, false, 200},
		{"expired_without_trust", -time.Hour, 0, false, 401},
		{"expired_trust_ended", -time.Hour, -time.Minute, false, 401},
		{"expired_with_trust", -time.Hour, time.Hour, false, 200},
		{"revoked_with_trust", -time.Hour, time.Hour, true, 401},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := NewManager("test-secret-long-enough")
			m.ttl = tc.ttl
			var trust *time.Time
			if tc.trust != 0 {
				v := time.Now().Add(tc.trust)
				trust = &v
			}
			token, err := m.IssueWithTrust(&models.User{ID: "u", Role: models.Role("admin")}, trust)
			if err != nil {
				t.Fatal(err)
			}
			if tc.revoked {
				m.SetRevokedAfter(func(string) time.Time { return time.Now().Add(time.Minute) })
			}
			claims, _, err := m.ParseWithTrust(token)
			permitted := err == nil && !m.Revoked(claims)
			if permitted != (tc.want == 200) {
				t.Fatalf("stream/SSE parse disagrees: %v", err)
			}
			for _, query := range []bool{false, true} {
				r := gin.New()
				r.GET("/", m.Middleware(), func(c *gin.Context) { c.Status(200) })
				url := "/"
				if query {
					url += "?token=" + token
				}
				req := httptest.NewRequest(http.MethodGet, url, nil)
				if !query {
					req.Header.Set("Authorization", "Bearer "+token)
				}
				w := httptest.NewRecorder()
				r.ServeHTTP(w, req)
				if w.Code != tc.want {
					t.Fatalf("query=%v status=%d want=%d", query, w.Code, tc.want)
				}
				renewed := w.Header().Get("X-Renewed-Token")
				if tc.want == 200 && tc.ttl < 0 && renewed == "" {
					t.Fatal("trusted expiry must renew")
				}
			}
		})
	}
}

func TestTrustNeverBypassesInvalidSignature(t *testing.T) {
	m := NewManager("correct-secret")
	claims := Claims{Sub: "u", TrustUntil: jwt.NewNumericDate(time.Now().Add(time.Hour)), RegisteredClaims: jwt.RegisteredClaims{ExpiresAt: jwt.NewNumericDate(time.Now().Add(-time.Hour))}}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte("wrong-secret"))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = m.ParseWithTrust(token); err == nil {
		t.Fatal("bad signature accepted")
	}
}
