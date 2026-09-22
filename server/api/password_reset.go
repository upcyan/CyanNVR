package api

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"math/big"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"cyannvr/server/models"
)

// 密码重置（忘记密码）流程
//
// 自托管服务没有邮件通道，因此以「能否读到服务器本地文件」作为身份依据：
//
//	1) POST /api/auth/reset-request  生成重置码，写入数据目录 reset-code.txt
//	   —— 网页上不显示重置码，用户必须到服务器上查看该文件
//	2) POST /api/auth/reset-verify   校验重置码；通过后立即销毁文件，
//	   并签发一次性临时凭证（grant）
//	3) POST /api/auth/reset-confirm  凭 grant 设置新密码
//
// 拆成三步的原因：要求「验证通过后再输入新密码」，因此校验成功的凭证必须与
// 重置码本身分离——重置码在验证时即销毁，避免被重复使用。
//
// 约束：重置码 5 分钟有效、到期由定时器主动删除文件；grant 5 分钟有效且
// 一次性；申请接口按 IP 限流。

const (
	resetCodeTTL     = 5 * time.Minute
	resetGrantTTL    = 5 * time.Minute
	resetCodeLen     = 8
	resetFileName    = "reset-code.txt"
	resetMaxRequests = 5
	resetReqWindow   = 10 * time.Minute
	resetMinPassword = 8
)

// 去掉易混淆字符（0/O/1/I/L），便于用户从终端手工转录
const resetCodeAlphabet = "23456789ABCDEFGHJKMNPQRSTUVWXYZ"

type codeEntry struct {
	Value   string
	Expires time.Time
}

type grantEntry struct {
	Value   string
	UserID  string
	Expires time.Time
}

type resetManager struct {
	mu    sync.Mutex
	code  *codeEntry
	grant *grantEntry
	path  string
	reqs  map[string][]time.Time
	timer *time.Timer
}

func newResetManager(dataDir string) *resetManager {
	return &resetManager{
		path: filepath.Join(dataDir, resetFileName),
		reqs: map[string][]time.Time{},
	}
}

// allow 按 IP 做滑动窗口限流，避免重置码被反复生成。
func (m *resetManager) allow(ip string) bool {
	now := time.Now()
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.reqs) > 1000 {
		for k, ts := range m.reqs {
			stale := true
			for _, t := range ts {
				if now.Sub(t) < resetReqWindow {
					stale = false
					break
				}
			}
			if stale {
				delete(m.reqs, k)
			}
		}
	}
	recent := m.reqs[ip][:0]
	for _, t := range m.reqs[ip] {
		if now.Sub(t) < resetReqWindow {
			recent = append(recent, t)
		}
	}
	if len(recent) >= resetMaxRequests {
		m.reqs[ip] = recent
		return false
	}
	m.reqs[ip] = append(recent, now)
	return true
}

// issue 生成新重置码并落盘，同时安排到期自动删除。
func (m *resetManager) issue() (*codeEntry, error) {
	code, err := randomCode(resetCodeLen)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	entry := &codeEntry{Value: code, Expires: now.Add(resetCodeTTL)}

	body := fmt.Sprintf(`CyanNVR 密码重置码
============================
重置码   : %s
生成时间 : %s
有效期至 : %s（%d 分钟内有效）

使用方法：
  1. 回到 CyanNVR 登录页的「重置密码」窗口
  2. 在倒计时结束前输入上面的重置码并提交验证
  3. 验证通过后本文件会立即被删除，随后设置新密码

注意：
  - 超过有效期本文件会被自动删除，需要重新生成
  - 此文件仅用于找回密码，请勿分享给他人
`, entry.Value,
		now.Format("2006-01-02 15:04:05"),
		entry.Expires.Format("2006-01-02 15:04:05"),
		int(resetCodeTTL.Minutes()))

	// 0600：仅服务运行用户可读
	if err := os.WriteFile(m.path, []byte(body), 0o600); err != nil {
		return nil, err
	}

	m.mu.Lock()
	m.code = entry
	// 新的重置码使任何未使用的 grant 作废
	m.grant = nil
	if m.timer != nil {
		m.timer.Stop()
	}
	// 到期主动销毁文件，而不是等下一次请求才发现过期
	m.timer = time.AfterFunc(resetCodeTTL, m.expireCode)
	m.mu.Unlock()

	return entry, nil
}

// expireCode 由定时器触发：若当前重置码仍未使用且已到期，删除文件并清理状态。
func (m *resetManager) expireCode() {
	m.mu.Lock()
	entry := m.code
	if entry == nil || time.Now().Before(entry.Expires) {
		m.mu.Unlock()
		return
	}
	m.code = nil
	m.mu.Unlock()

	_ = os.Remove(m.path)
	log.Printf("password reset code expired, removed %s", m.path)
}

// verify 校验重置码。通过后立即销毁文件与重置码，并签发一次性 grant。
// 返回 grant 值、过期时间与是否成功。
func (m *resetManager) verify(code, userID string) (string, time.Time, bool) {
	m.mu.Lock()
	entry := m.code
	if entry == nil {
		m.mu.Unlock()
		return "", time.Time{}, false
	}
	if time.Now().After(entry.Expires) {
		m.code = nil
		m.mu.Unlock()
		_ = os.Remove(m.path)
		return "", time.Time{}, false
	}
	// 常量时间比较，避免逐字符计时侧信道
	if subtleCompare(strings.ToUpper(strings.TrimSpace(code)), entry.Value) != 1 {
		m.mu.Unlock()
		return "", time.Time{}, false
	}

	// 校验通过：销毁重置码与文件
	m.code = nil
	if m.timer != nil {
		m.timer.Stop()
		m.timer = nil
	}
	m.mu.Unlock()

	_ = os.Remove(m.path)

	grant, err := randomHex(32)
	if err != nil {
		return "", time.Time{}, false
	}
	exp := time.Now().Add(resetGrantTTL)

	m.mu.Lock()
	m.grant = &grantEntry{Value: grant, UserID: userID, Expires: exp}
	m.mu.Unlock()

	log.Printf("password reset code verified for user id=%s, grant expires %s",
		userID, exp.Format(time.RFC3339))
	return grant, exp, true
}

// consumeGrant 校验并一次性消费 grant，返回其绑定的用户 ID。
func (m *resetManager) consumeGrant(grant string) (string, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	g := m.grant
	if g == nil {
		return "", false
	}
	if time.Now().After(g.Expires) {
		m.grant = nil
		return "", false
	}
	if subtleCompare(strings.TrimSpace(grant), g.Value) != 1 {
		return "", false
	}
	userID := g.UserID
	m.grant = nil
	return userID, true
}

// discard 使当前重置码与 grant 全部失效。
func (m *resetManager) discard() {
	m.mu.Lock()
	m.code = nil
	m.grant = nil
	if m.timer != nil {
		m.timer.Stop()
		m.timer = nil
	}
	m.mu.Unlock()
	_ = os.Remove(m.path)
}

func randomCode(n int) (string, error) {
	max := big.NewInt(int64(len(resetCodeAlphabet)))
	out := make([]byte, n)
	for i := range out {
		idx, err := rand.Int(rand.Reader, max)
		if err != nil {
			return "", err
		}
		out[i] = resetCodeAlphabet[idx.Int64()]
	}
	return string(out), nil
}

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// subtleCompare 常量时间字符串比较：相等返回 1，否则 0。
func subtleCompare(a, b string) int {
	if len(a) != len(b) {
		return 0
	}
	var diff byte
	for i := 0; i < len(a); i++ {
		diff |= a[i] ^ b[i]
	}
	if diff == 0 {
		return 1
	}
	return 0
}

// ---- HTTP handlers ----

// resetRequest 生成重置码。无需鉴权：能读到服务器文件才算真正持有者。
func (s *Server) resetRequest(c *gin.Context) {
	if !s.reset.allow(c.ClientIP()) {
		c.JSON(http.StatusTooManyRequests, gin.H{"error": "请求过于频繁，请稍后再试"})
		return
	}
	entry, err := s.reset.issue()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "生成重置码失败"})
		return
	}
	log.Printf("password reset code issued (expires %s), written to %s",
		entry.Expires.Format(time.RFC3339), s.reset.path)
	c.JSON(http.StatusOK, gin.H{
		"ok":        true,
		"file":      s.reset.path,
		"expiresAt": entry.Expires.Format(time.RFC3339),
		"ttl":       int(resetCodeTTL.Seconds()),
	})
}

type resetVerifyReq struct {
	Code     string `json:"code"`
	Username string `json:"username"`
}

// resetVerify 校验重置码并签发一次性 grant（用于后续设置新密码）。
func (s *Server) resetVerify(c *gin.Context) {
	var req resetVerifyReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}
	req.Code = strings.TrimSpace(req.Code)
	if req.Code == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请输入重置码"})
		return
	}

	u, err := s.pickResetUser(strings.TrimSpace(req.Username))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	grant, exp, ok := s.reset.verify(req.Code, u.ID)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "重置码无效或已过期"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"ok":        true,
		"grant":     grant,
		"user":      u.Username,
		"expiresAt": exp.Format(time.RFC3339),
		"ttl":       int(resetGrantTTL.Seconds()),
	})
}

type resetConfirmReq struct {
	Grant    string `json:"grant"`
	Password string `json:"password"`
}

// resetConfirm 凭 grant 设置新密码。
func (s *Server) resetConfirm(c *gin.Context) {
	var req resetConfirmReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}
	if strings.TrimSpace(req.Grant) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请先验证重置码"})
		return
	}
	if len(req.Password) < resetMinPassword {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("密码至少需要 %d 个字符", resetMinPassword)})
		return
	}

	userID, ok := s.reset.consumeGrant(req.Grant)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "验证已失效，请重新获取重置码"})
		return
	}

	u, err := s.st.GetUserByID(userID)
	if err != nil || u == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "用户不存在"})
		return
	}

	hash, err := s.am.HashPassword(req.Password)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	if err := s.st.UpdateUserPassword(u.ID, hash); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "更新密码失败"})
		return
	}

	log.Printf("password reset completed for user %q (id=%s) from %s", u.Username, u.ID, c.ClientIP())
	c.JSON(http.StatusOK, gin.H{
		"ok":      true,
		"user":    u.Username,
		"message": "密码已重置，请使用新密码登录",
	})
}

// pickResetUser 选定要重置的用户：指定用户名优先，否则回退到唯一管理员。
// 存在多个管理员时必须显式指定，避免误改。
func (s *Server) pickResetUser(name string) (*models.User, error) {
	if name != "" {
		u, err := s.st.GetUserByName(name)
		if err != nil {
			return nil, fmt.Errorf("查询用户失败")
		}
		if u == nil {
			return nil, fmt.Errorf("用户不存在：%s（当前用户名可在应用设置页或 cyannvr list-users 查看）", name)
		}
		return u, nil
	}

	users, err := s.st.ListUsers()
	if err != nil {
		return nil, fmt.Errorf("查询用户失败")
	}
	var admins []models.User
	for _, u := range users {
		if u.Role == models.RoleAdmin {
			admins = append(admins, u)
		}
	}
	switch len(admins) {
	case 0:
		return nil, fmt.Errorf("系统中没有管理员账号，请指定用户名")
	case 1:
		return &admins[0], nil
	default:
		names := make([]string, 0, len(admins))
		for _, a := range admins {
			names = append(names, a.Username)
		}
		return nil, fmt.Errorf("存在多个管理员（%s），请填写要重置的用户名", strings.Join(names, "、"))
	}
}
