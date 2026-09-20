package api

import (
	"crypto/tls"
	"net/http"

	"github.com/gin-gonic/gin"

	"cyannvr/server/pkg/tlsx"
)

// SetTLSRunner 注入 HTTPS 运行器（main 中构建 router 后调用）。
func (s *Server) SetTLSRunner(r *tlsx.Runner) { s.tls = r }

// CurrentTLSConfig 从当前设置读取 TLS 目标配置，供运行器热生效。
func (s *Server) CurrentTLSConfig() tlsx.Config {
	s.settingsMu.Lock()
	defer s.settingsMu.Unlock()
	set := *s.settings
	port := set.HTTPSPort
	return tlsx.Config{
		Enabled: set.HTTPS,
		Port:    port,
		Mode:    set.TLSCertMode,
		Domain:  set.TLSDomain,
		Email:   set.ACMEEmail,
	}
}

func (s *Server) tlsStatus(c *gin.Context) {
	st := s.tls.Status()
	c.JSON(http.StatusOK, st)
}

func (s *Server) uploadManualCert(c *gin.Context) {
	var in struct {
		Cert string `json:"cert"`
		Key  string `json:"key"`
	}
	if err := c.ShouldBindJSON(&in); err != nil || in.Cert == "" || in.Key == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "需要 cert 与 key 字段（PEM 文本）"})
		return
	}
	if _, err := tls.X509KeyPair([]byte(in.Cert), []byte(in.Key)); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "证书与私钥不匹配或格式无效: " + err.Error()})
		return
	}
	st, err := s.tls.SaveManualCert([]byte(in.Cert), []byte(in.Key))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	// 手动模式下热重启，让新证书立即生效
	go s.tls.Apply(s.CurrentTLSConfig())
	c.JSON(http.StatusOK, st)
}

func (s *Server) reloadTLS(c *gin.Context) {
	go s.tls.Apply(s.CurrentTLSConfig())
	c.JSON(http.StatusOK, gin.H{"ok": true})
}
