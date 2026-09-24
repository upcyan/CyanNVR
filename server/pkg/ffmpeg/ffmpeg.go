package ffmpeg

import (
	"bytes"
	"context"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
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

// probeTimeout 是连通性/编码探测的硬上限。
// -t 1 只限制「输出」时长，对 RTSP 建连阶段无效：目标不可达、TCP 半开
// 或相机不响应 DESCRIBE 时，ffmpeg 会长时间挂住不退出。这里给进程级硬超时，
// 超时即整组强杀，避免「测试连接」卡死界面、也避免探测进程累积泄漏。
const probeTimeout = 8 * time.Second

// runProbe 执行一次 ffmpeg 探测并返回合并后的 stderr，超时返回 timedOut=true。
func runProbe(bin string, args []string) (msg string, err error, timedOut bool) {
	var out bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Stdout = &out
	cmd.Stderr = &out
	// 独立进程组 + 超时后强杀整组：ffmpeg 可能派生线程/子进程，
	// 只杀主进程会留下孤儿。WaitDelay 保证 Kill 后不会阻塞在管道读取上。
	cmd.SysProcAttr = sysProcAttr()
	cmd.WaitDelay = 2 * time.Second
	err = cmd.Run()
	if ctx.Err() == context.DeadlineExceeded {
		return out.String(), err, true
	}
	return out.String(), err, false
}

// TestRTSPErr performs an RTSP connectivity test and returns a human-readable
// error reason (auth failure, timeout, connection refused, binary missing).
func TestRTSPErr(bin, url string) (bool, string) {
	ok, errMsg, _, _, _ := TestRTSPProbe(bin, url)
	return ok, errMsg
}

// TestRTSPProbe 在连通性测试的同时解析视频编码与分辨率。
// 「测试连接」用它一步发现 H.265（浏览器播不了、录像必须转码）的摄像头。
func TestRTSPProbe(bin, url string) (ok bool, errMsg, codec string, width, height int) {
	if !Exists(bin) {
		return false, "ffmpeg 未安装或未配置", "", 0, 0
	}
	args := []string{"-hide_banner"}
	// -rtsp_transport 只对 RTSP 输入有意义；对 http/file 等其它协议加了会
	// 直接报 "Option rtsp_transport not found"，把本可探测的源误判为失败。
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(url)), "rtsp://") {
		args = append(args, "-rtsp_transport", "tcp")
	}
	args = append(args, "-i", url, "-t", "1", "-f", "null", "-")
	msg, err, timedOut := runProbe(bin, args)
	codec, width, height = parseVideoStream(msg)
	if timedOut {
		return false, "探测超时（12 秒无响应）：请检查网络连通性与摄像头是否可达", codec, width, height
	}
	if err == nil {
		return true, "", codec, width, height
	}
	switch {
	case containsFold(msg, "401"):
		return false, "认证失败，请检查用户名/密码（注意大小写；若密码无误，相机可能已连续错试锁定该账号）", codec, width, height
	case containsFold(msg, "404"):
		return false, "未找到该码流地址（/stream1 不正确）", codec, width, height
	case containsFold(msg, "connection timed out"), containsFold(msg, "timed out"):
		return false, "连接超时，请检查 IP 与端口", codec, width, height
	case containsFold(msg, "connection refused"):
		return false, "连接被拒绝，请确认设备 RTSP 端口", codec, width, height
	case containsFold(msg, "no route to host"):
		return false, "网络不可达，请检查网络", codec, width, height
	case containsFold(msg, "invalid data"):
		return false, "地址格式错误", codec, width, height
	}
	return false, "无法连接：" + firstLine(msg), codec, width, height
}

// parseVideoStream 从 ffmpeg stderr 中提取视频编码与分辨率。
// 典型行：Stream #0:0[0x100](eng): Video: h264 (High), yuv420p(progressive), 1920x1080, ...
func parseVideoStream(msg string) (codec string, width, height int) {
	for _, line := range strings.Split(msg, "\n") {
		idx := strings.Index(line, "Video:")
		if idx < 0 {
			continue
		}
		seg := line[idx+len("Video:"):]
		// 编码名：第一个逗号前的第一段
		name := seg
		if c := strings.IndexByte(name, ','); c >= 0 {
			name = name[:c]
		}
		lower := strings.ToLower(name)
		for _, cand := range []string{"hevc", "h264", "h265", "mpeg4", "vp8", "vp9", "av1"} {
			if strings.Contains(lower, cand) {
				codec = cand
				break
			}
		}
		if codec == "h265" {
			codec = "hevc"
		}
		// 分辨率：形如 ", 1920x1080, " 的第一处
		for i := 0; i+1 < len(seg); i++ {
			if seg[i] < '0' || seg[i] > '9' {
				continue
			}
			j := i
			for j < len(seg) && seg[j] >= '0' && seg[j] <= '9' {
				j++
			}
			if j < len(seg) && seg[j] == 'x' {
				k := j + 1
				for k < len(seg) && seg[k] >= '0' && seg[k] <= '9' {
					k++
				}
				if k > j+1 && (k == len(seg) || seg[k] == ',' || seg[k] == ' ' || seg[k] == '(') {
					w, _ := strconv.Atoi(seg[i:j])
					h, _ := strconv.Atoi(seg[j+1 : k])
					if w > 0 && h > 0 {
						return codec, w, h
					}
				}
			}
			i = j
		}
		return codec, 0, 0
	}
	return "", 0, 0
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
