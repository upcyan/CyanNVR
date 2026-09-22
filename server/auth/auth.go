package auth

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"

	"cyannvr/server/models"
)

const ctxUserKey = "auth_user"

var ErrInvalid = errors.New("invalid credentials")

type Claims struct {
	Sub  string `json:"sub"`
	Role string `json:"role"`
	jwt.RegisteredClaims
}

type Manager struct {
	secret []byte
	ttl    time.Duration
	// revokedAfter 由上层注入：给定用户 ID，返回「早于此时刻签发的 token 一律失效」
	// 的时间点（即该用户最后一次改密时间）。返回零值表示不做吊销校验。
	// 用回调而不是直接依赖 store，是为了让 auth 包保持无存储依赖、便于测试。
	revokedAfter func(userID string) time.Time
}

func NewManager(secret string) *Manager {
	return &Manager{secret: []byte(secret), ttl: 24 * time.Hour}
}

// SetRevokedAfter 注册改密时间查询函数，启用「改密即注销旧会话」。
func (m *Manager) SetRevokedAfter(fn func(userID string) time.Time) {
	m.revokedAfter = fn
}

// revoked 判断 token 的签发时间是否早于该用户最后一次改密时间。
func (m *Manager) Revoked(claims *Claims) bool {
	if m.revokedAfter == nil || claims == nil {
		return false
	}
	cutoff := m.revokedAfter(claims.Sub)
	if cutoff.IsZero() {
		return false
	}
	iat := claims.IssuedAt
	if iat == nil {
		// 没有签发时间的 token 无法判断新旧，保守拒绝
		return true
	}
	// 允许 1 秒误差：同一秒内「改密 + 重新登录」不应被误判为失效
	return iat.Time.Before(cutoff.Add(-time.Second))
}

func (m *Manager) HashPassword(pw string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.DefaultCost)
	return string(b), err
}

func (m *Manager) VerifyPassword(hash, pw string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(pw)) == nil
}

func (m *Manager) Issue(u *models.User) (string, error) {
	now := time.Now()
	claims := Claims{
		Sub:  u.ID,
		Role: string(u.Role),
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(now.Add(m.ttl)),
			IssuedAt:  jwt.NewNumericDate(now),
			Subject:   u.ID,
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(m.secret)
}

func (m *Manager) Parse(tokenStr string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(t *jwt.Token) (any, error) {
		return m.secret, nil
	}, jwt.WithValidMethods([]string{"HS256"}))
	if err != nil {
		return nil, err
	}
	if claims, ok := token.Claims.(*Claims); ok && token.Valid {
		return claims, nil
	}
	return nil, ErrInvalid
}

// AuthUser is injected into context by middleware.
type AuthUser struct {
	ID   string
	Role models.Role
}

func (m *Manager) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		token := bearerToken(c)
		if token == "" {
			token = c.Query("token")
		}
		if token == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "missing token"})
			return
		}
		claims, err := m.Parse(token)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid token"})
			return
		}
		// 改密后旧 token 立即失效：早于 password_changed_at 签发的一律拒绝
		if m.Revoked(claims) {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "密码已变更，请重新登录"})
			return
		}
		c.Set(ctxUserKey, &AuthUser{ID: claims.Sub, Role: models.Role(claims.Role)})
		c.Next()
	}
}

func bearerToken(c *gin.Context) string {
	h := c.GetHeader("Authorization")
	if h == "" {
		return ""
	}
	parts := strings.SplitN(h, " ", 2)
	if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
		return strings.TrimSpace(parts[1])
	}
	return ""
}

func CurrentUser(c *gin.Context) *AuthUser {
	if v, ok := c.Get(ctxUserKey); ok {
		if u, ok := v.(*AuthUser); ok {
			return u
		}
	}
	return nil
}

// RequireRole allows only the given roles (empty = any authenticated).
func RequireRole(roles ...models.Role) gin.HandlerFunc {
	return func(c *gin.Context) {
		u := CurrentUser(c)
		if u == nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}
		if len(roles) > 0 {
			ok := false
			for _, r := range roles {
				if u.Role == r {
					ok = true
					break
				}
			}
			if !ok {
				c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "forbidden"})
				return
			}
		}
		c.Next()
	}
}

func IsAdmin(u *AuthUser) bool { return u != nil && u.Role == models.RoleAdmin }
