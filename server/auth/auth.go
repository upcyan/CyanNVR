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
	// TrustUntil 免登录信任窗口截止时刻（nil=无窗口）。token 过期但
	// 仍在此窗口内时，中间件会自动续签新 token 放行，避免频繁重登。
	TrustUntil *jwt.NumericDate `json:"tw,omitempty"`
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
	return m.IssueWithTrust(u, nil)
}

// IssueWithTrust 签发带信任窗口的 token。trustUntil 为 nil 表示无窗口。
func (m *Manager) IssueWithTrust(u *models.User, trustUntil *time.Time) (string, error) {
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
	if trustUntil != nil && trustUntil.After(now) {
		claims.TrustUntil = jwt.NewNumericDate(*trustUntil)
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
		expired := false
		if err != nil {
			// 区分「签名无效」与「已过期」：仅过期可被信任窗口豁免
			if !isExpiredErr(err) {
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid token"})
				return
			}
			claims, err = m.ParseLenient(token)
			if err != nil {
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid token"})
				return
			}
			expired = true
		}
		// 改密后旧 token 立即失效：早于 password_changed_at 签发的一律拒绝。
		// 信任窗口不豁免这一条——改密必须重新输入密码。
		if m.Revoked(claims) {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "密码已变更，请重新登录"})
			return
		}
		c.Set(ctxUserKey, &AuthUser{ID: claims.Sub, Role: models.Role(claims.Role)})
		// 过期但仍在信任窗口内：签发新 token 经响应头下发，前端替换存储，
		// 用户全程无感知；请求本身照常放行。
		if expired && claims.TrustUntil != nil && time.Now().Before(claims.TrustUntil.Time) {
			if renewed, err := m.Renew(claims); err == nil {
				c.Header("X-Renewed-Token", renewed)
			}
		}
		c.Next()
	}
}

// isExpiredErr 判断 jwt 解析错误是否为「签名有效但已过期」。
func isExpiredErr(err error) bool {
	return err != nil && strings.Contains(err.Error(), "token is expired")
}

// ParseLenient 解析过期 token（仅用于信任窗口判定）：校验签名但不校验 exp。
func (m *Manager) ParseLenient(tokenStr string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(t *jwt.Token) (any, error) {
		return m.secret, nil
	}, jwt.WithValidMethods([]string{"HS256"}), jwt.WithoutClaimsValidation())
	if err != nil {
		return nil, err
	}
	if claims, ok := token.Claims.(*Claims); ok {
		return claims, nil
	}
	return nil, ErrInvalid
}

// Renew 为仍在信任窗口内的过期会话签发等权新 token（信任窗口顺延一轮）。
func (m *Manager) Renew(old *Claims) (string, error) {
	now := time.Now()
	claims := Claims{
		Sub:        old.Sub,
		Role:       old.Role,
		TrustUntil: old.TrustUntil,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(now.Add(m.ttl)),
			IssuedAt:  jwt.NewNumericDate(now),
			Subject:   old.Sub,
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(m.secret)
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
