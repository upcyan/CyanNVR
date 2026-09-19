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
}

func NewManager(secret string) *Manager {
	return &Manager{secret: []byte(secret), ttl: 24 * time.Hour}
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
