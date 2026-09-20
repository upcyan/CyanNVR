package tlsx

import (
	"crypto/tls"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"golang.org/x/crypto/acme/autocert"
)

// Runner 管理 CyanNVR 的 HTTPS 监听：
//   - manual 模式：使用 data/tls/cert.pem + key.pem（网页端手动上传）
//   - auto   模式：ACME 自动申请（Let's Encrypt），HTTP-01 走 80 端口，
//                  80 不可用时退回 TLS-ALPN-01（仅需 443 可达）
//
// HTTP 主服务（默认 8080）始终独立运行，HTTPS 的启停不影响局域网访问。
type Runner struct {
	mu      sync.Mutex
	dataDir string
	handler http.Handler
	cfg     Config

	srv    *http.Server
	acme80 *http.Server
	status Status
}

type Config struct {
	Enabled bool
	Port    int
	Mode    string // "auto" | "manual"
	Domain  string
	Email   string
}

type Status struct {
	Enabled   bool   `json:"enabled"`
	Running   bool   `json:"running"`
	Mode      string `json:"mode"`
	Domain    string `json:"domain"`
	Port      int    `json:"port"`
	HasManual bool   `json:"hasManual"`
	NotAfter  string `json:"notAfter,omitempty"`
	Issuer    string `json:"issuer,omitempty"`
	Message   string `json:"message,omitempty"`
}

func New(dataDir string, handler http.Handler) *Runner {
	r := &Runner{dataDir: dataDir, handler: handler}
	r.status = r.snapshot(cfgOf(r.status))
	return r
}

func (r *Runner) certDir() string  { return filepath.Join(r.dataDir, "tls") }
func (r *Runner) certPath() string { return filepath.Join(r.certDir(), "cert.pem") }
func (r *Runner) keyPath() string  { return filepath.Join(r.certDir(), "key.pem") }

// Apply 按目标配置重启 HTTPS 监听；失败时保留 HTTP 服务并记录 Message。
func (r *Runner) Apply(cfg Config) {
	if cfg.Port == 0 {
		cfg.Port = 443
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.stopLocked()
	r.cfg = cfg

	st := r.snapshot(cfg)
	switch {
	case !cfg.Enabled:
		// 已停用，无需动作
	case cfg.Mode == "manual":
		r.startManual(&st, cfg)
	case cfg.Mode == "auto":
		r.startAuto(&st, cfg)
	default:
		st.Message = "请选择证书模式"
	}
	r.status = st
	if st.Running {
		log.Printf("HTTPS listening on :%d (%s, domain=%s)", cfg.Port, cfg.Mode, cfg.Domain)
	} else if cfg.Enabled {
		log.Printf("HTTPS not running: %s", st.Message)
	}
}

func (r *Runner) startManual(st *Status, cfg Config) {
	cert, err := loadKeyPair(r.certPath(), r.keyPath())
	if err != nil {
		st.Message = "尚未上传证书或证书无效"
		return
	}
	fillCertInfo(st, cert)
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", cfg.Port))
	if err != nil {
		st.Message = fmt.Sprintf("监听 %d 端口失败: %v", cfg.Port, err)
		return
	}
	srv := &http.Server{Handler: r.handler, TLSConfig: &tls.Config{Certificates: []tls.Certificate{*cert}}}
	go r.serve(srv, ln)
	st.Running = true
}

func (r *Runner) startAuto(st *Status, cfg Config) {
	if cfg.Domain == "" {
		st.Message = "自动申请需要先填写域名"
		return
	}
	m := &autocert.Manager{
		Cache:      autocert.DirCache(filepath.Join(r.certDir(), "acme")),
		Prompt:     autocert.AcceptTOS,
		HostPolicy: autocert.HostWhitelist(cfg.Domain),
		Email:      cfg.Email,
	}
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", cfg.Port))
	if err != nil {
		st.Message = fmt.Sprintf("监听 %d 端口失败: %v", cfg.Port, err)
		return
	}
	srv := &http.Server{Handler: r.handler, TLSConfig: m.TLSConfig()}
	go r.serve(srv, ln)
	r.srv = srv

	// HTTP-01 验证需要 80 端口；被占用/不可达时退回 TLS-ALPN-01（443 可达即可）
	h := &http.Server{Addr: ":80", Handler: m.HTTPHandler(nil)}
	if ln80, err := net.Listen("tcp", ":80"); err == nil {
		go func() {
			if err := h.Serve(ln80); err != nil && !errors.Is(err, http.ErrServerClosed) {
				log.Printf("ACME :80 handler stopped: %v", err)
			}
		}()
		r.acme80 = h
	} else {
		st.Message = "80 端口不可用，将使用 TLS-ALPN 验证（需 443 可达）"
	}
	st.Running = true

	// 异步触发首次申请，尽快把结果反馈到状态接口
	domain := cfg.Domain
	go func() {
		c, err := m.GetCertificate(&tls.ClientHelloInfo{ServerName: domain})
		if err != nil {
			r.mu.Lock()
			r.status.Message = "证书申请未完成: " + err.Error()
			r.mu.Unlock()
			return
		}
		r.mu.Lock()
		fillCertInfo(&r.status, c)
		r.status.Message = "证书已就绪"
		r.mu.Unlock()
	}()
}

func (r *Runner) serve(srv *http.Server, ln net.Listener) {
	if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Printf("HTTPS server stopped: %v", err)
		r.mu.Lock()
		r.status.Running = false
		r.status.Message = err.Error()
		r.mu.Unlock()
	}
}

func (r *Runner) stopLocked() {
	if r.srv != nil {
		_ = r.srv.Close()
		r.srv = nil
	}
	if r.acme80 != nil {
		_ = r.acme80.Close()
		r.acme80 = nil
	}
}

func (r *Runner) snapshot(cfg Config) Status {
	st := Status{
		Enabled: cfg.Enabled,
		Mode:    cfg.Mode,
		Domain:  cfg.Domain,
		Port:    cfg.Port,
	}
	if st.Port == 0 {
		st.Port = 443
	}
	if cert, err := loadKeyPair(r.certPath(), r.keyPath()); err == nil {
		st.HasManual = true
		fillCertInfo(&st, cert)
	}
	return st
}

// SaveManualCert 校验并持久化手动上传的证书对。
func (r *Runner) SaveManualCert(certPEM, keyPEM []byte) (Status, error) {
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return r.Status(), fmt.Errorf("证书与私钥不匹配或格式无效: %w", err)
	}
	if err := os.MkdirAll(r.certDir(), 0o700); err != nil {
		return r.Status(), err
	}
	if err := os.WriteFile(r.certPath(), certPEM, 0o644); err != nil {
		return r.Status(), err
	}
	if err := os.WriteFile(r.keyPath(), keyPEM, 0o600); err != nil {
		return r.Status(), err
	}
	r.mu.Lock()
	st := r.snapshot(r.cfg)
	fillCertInfo(&st, &cert)
	st.HasManual = true
	r.status = st
	r.mu.Unlock()
	return st, nil
}

// Status 返回当前状态快照。
func (r *Runner) Status() Status {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.status
}

func cfgOf(st Status) Config {
	return Config{Enabled: st.Enabled, Port: st.Port, Mode: st.Mode, Domain: st.Domain}
}

func loadKeyPair(certPath, keyPath string) (*tls.Certificate, error) {
	certPEM, err := os.ReadFile(certPath)
	if err != nil {
		return nil, err
	}
	keyPEM, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, err
	}
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, err
	}
	return &cert, nil
}

func fillCertInfo(st *Status, cert *tls.Certificate) {
	if cert.Leaf == nil {
		return
	}
	st.NotAfter = cert.Leaf.NotAfter.Format(time.RFC3339)
	if len(cert.Leaf.Subject.Organization) > 0 {
		st.Issuer = cert.Leaf.Subject.Organization[0]
	}
}
