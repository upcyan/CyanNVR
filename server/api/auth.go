package api

import (
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"simplenvr/server/auth"
	"simplenvr/server/models"
)

type loginReq struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// loginRateLimiter is a simple per-IP sliding-window limiter for the login
// endpoint: max 10 attempts per minute per IP.
type loginRateLimiter struct {
	mu      sync.Mutex
	attempts map[string][]time.Time
}

var loginLimiter = &loginRateLimiter{attempts: map[string][]time.Time{}}

const (
	loginMaxAttempts  = 10
	loginWindowPeriod = time.Minute
)

func (l *loginRateLimiter) allow(ip string) bool {
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	recent := l.attempts[ip][:0]
	for _, t := range l.attempts[ip] {
		if now.Sub(t) < loginWindowPeriod {
			recent = append(recent, t)
		}
	}
	if len(recent) >= loginMaxAttempts {
		l.attempts[ip] = recent
		return false
	}
	l.attempts[ip] = append(recent, now)
	return true
}

func (s *Server) login(c *gin.Context) {
	ip := c.ClientIP()
	if !loginLimiter.allow(ip) {
		c.JSON(http.StatusTooManyRequests, gin.H{"error": "尝试过于频繁，请稍后再试"})
		return
	}
	var req loginReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}
	u, err := s.st.GetUserByName(req.Username)
	if err != nil || u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "用户名或密码错误"})
		return
	}
	if !s.am.VerifyPassword(u.PasswordHash, req.Password) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "用户名或密码错误"})
		return
	}
	token, err := s.am.Issue(u)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "token error"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"token": token, "user": publicUser(u)})
}

func (s *Server) me(c *gin.Context) {
	au := auth.CurrentUser(c)
	if au == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	u, _ := s.st.GetUserByID(au.ID)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"user": publicUser(u)})
}

func publicUser(u *models.User) gin.H {
	return gin.H{
		"id":        u.ID,
		"username":  u.Username,
		"role":      u.Role,
		"createdAt": u.CreatedAt,
	}
}

func (s *Server) listUsers(c *gin.Context) {
	users, err := s.st.ListUsers()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	out := make([]gin.H, 0, len(users))
	for _, u := range users {
		out = append(out, publicUser(&u))
	}
	c.JSON(http.StatusOK, gin.H{"users": out})
}

type userReq struct {
	Username string     `json:"username"`
	Password string     `json:"password"`
	Role     models.Role `json:"role"`
}

func (s *Server) createUser(c *gin.Context) {
	var req userReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}
	if req.Username == "" || req.Password == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "username/password required"})
		return
	}
	if req.Role == "" {
		req.Role = models.RoleUser
	}
	hash, err := s.am.HashPassword(req.Password)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	u := models.User{
		ID:           uuid.NewString(),
		Username:     req.Username,
		PasswordHash: hash,
		Role:         req.Role,
		CreatedAt:    time.Now(),
	}
	if err := s.st.CreateUser(u); err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": "用户名已存在"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"user": publicUser(&u)})
}

func (s *Server) updateUser(c *gin.Context) {
	id := c.Param("id")
	var req struct {
		Password string     `json:"password"`
		Role     models.Role `json:"role"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}
	u, _ := s.st.GetUserByID(id)
	if u == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
		return
	}
	if req.Password != "" {
		hash, err := s.am.HashPassword(req.Password)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		_ = s.st.UpdateUserPassword(id, hash)
	}
	if req.Role != "" {
		_ = s.st.UpdateUserRole(id, req.Role)
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (s *Server) deleteUser(c *gin.Context) {
	id := c.Param("id")
	au := auth.CurrentUser(c)
	if au.ID == id {
		c.JSON(http.StatusBadRequest, gin.H{"error": "不能删除自己"})
		return
	}
	if err := s.st.DeleteUser(id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (s *Server) changeOwnPassword(c *gin.Context) {
	var req struct {
		Old string `json:"old"`
		New string `json:"new"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}
	au := auth.CurrentUser(c)
	u, _ := s.st.GetUserByID(au.ID)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	if !s.am.VerifyPassword(u.PasswordHash, req.Old) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "原密码错误"})
		return
	}
	hash, err := s.am.HashPassword(req.New)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	_ = s.st.UpdateUserPassword(au.ID, hash)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (s *Server) requireAdmin(c *gin.Context) {
	u := auth.CurrentUser(c)
	if u == nil || u.Role != models.RoleAdmin {
		c.JSON(http.StatusForbidden, gin.H{"error": "需要管理员权限"})
		c.Abort()
		return
	}
	c.Next()
}

func (s *Server) requireOperator(c *gin.Context) {
	u := auth.CurrentUser(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		c.Abort()
		return
	}
	if u.Role == models.RoleViewer {
		c.JSON(http.StatusForbidden, gin.H{"error": "只读账号无权操作"})
		c.Abort()
		return
	}
	c.Next()
}
