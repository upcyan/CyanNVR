package ffmpeg

import (
	"bytes"
	"os/exec"
	"strings"
	"sync"
)

var lock sync.Mutex
var lastLog bytes.Buffer

func Exists(bin string) bool {
	_, err := exec.LookPath(bin)
	return err == nil
}

type Proc struct {
	cmd    *exec.Cmd
	stdout bytes.Buffer
	stderr bytes.Buffer
	done   chan error
	killed bool
	mu     sync.Mutex
}

func Start(bin string, args ...string) (*Proc, error) {
	cmd := exec.Command(bin, args...)
	p := &Proc{cmd: cmd, done: make(chan error, 1)}
	cmd.Stdout = &p.stdout
	cmd.Stderr = &p.stderr
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	go func() { p.done <- cmd.Wait() }()
	return p, nil
}

func (p *Proc) Wait() error { return <-p.done }

func (p *Proc) Running() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return !p.killed && p.cmd.ProcessState == nil
}

func (p *Proc) Kill() {
	p.mu.Lock()
	p.killed = true
	p.mu.Unlock()
	if p.cmd.Process != nil {
		_ = p.cmd.Process.Kill()
	}
}

func (p *Proc) Log() string {
	return p.stderr.String() + p.stdout.String()
}

func (p *Proc) Pid() int {
	if p.cmd.Process != nil {
		return p.cmd.Process.Pid
	}
	return 0
}

// TestRTSPErr performs an RTSP connectivity test and returns a human-readable
// error reason (auth failure, timeout, connection refused, binary missing).
func TestRTSPErr(bin, url string) (bool, string) {
	if !Exists(bin) {
		return false, "ffmpeg 未安装或未配置"
	}
	var out bytes.Buffer
	cmd := exec.Command(bin, "-rtsp_transport", "tcp", "-i", url, "-t", "3", "-f", "null", "-")
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	if err == nil {
		return true, ""
	}
	msg := out.String()
	switch {
	case containsFold(msg, "401"):
		return false, "认证失败，请检查用户名/密码"
	case containsFold(msg, "404"):
		return false, "未找到该码流地址（/stream1 不正确）"
	case containsFold(msg, "connection timed out"), containsFold(msg, "timed out"):
		return false, "连接超时，请检查 IP 与端口"
	case containsFold(msg, "connection refused"):
		return false, "连接被拒绝，请确认设备 RTSP 端口"
	case containsFold(msg, "no route to host"):
		return false, "网络不可达，请检查网络"
	case containsFold(msg, "invalid data"):
		return false, "地址格式错误"
	}
	return false, "无法连接：" + firstLine(msg)
}

// ProbeVideoCodec detects the video codec of an RTSP (or file) input by
// reading ffmpeg's stream info. Returns "h264", "hevc" or "" on failure.
func ProbeVideoCodec(bin, url string) string {
	var out bytes.Buffer
	cmd := exec.Command(bin, "-hide_banner", "-rtsp_transport", "tcp", "-i", url, "-t", "1", "-f", "null", "-")
	cmd.Stdout = &out
	cmd.Stderr = &out
	_ = cmd.Run()
	msg := out.String()
	// Match the "Stream #0:0: Video: hevc (Main)..." line
	for _, line := range strings.Split(msg, "\n") {
		if !strings.Contains(line, "Video:") {
			continue
		}
		lower := strings.ToLower(line)
		for _, codec := range []string{"hevc", "h264", "mpeg4", "h265"} {
			if strings.Contains(lower, "video: "+codec) || strings.Contains(lower, "video: "+codec+" ") {
				if codec == "h265" {
					return "hevc"
				}
				return codec
			}
		}
	}
	return ""
}

func containsFold(s, sub string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(sub))
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}
