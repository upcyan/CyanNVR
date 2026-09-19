package ffmpeg

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// EncoderKind 表示实际选用的 H.264 编码器。
type EncoderKind string

const (
	// EncoderNVENC NVIDIA 硬件编码（Tesla P4 等）。
	EncoderNVENC EncoderKind = "h264_nvenc"
	// EncoderVAAPI VAAPI 硬件编码（AMD/Intel 核显）。
	EncoderVAAPI EncoderKind = "h264_vaapi"
	// EncoderSoftware 软件编码，始终可用的兜底方案。
	EncoderSoftware EncoderKind = "libx264"
)

var (
	hwOnce   sync.Once
	hwKind   EncoderKind
	hwDesc   string
	hwVAAPID string
)

// ProbeH264Encoder 探测可用的 H.264 编码器，结果只计算一次并缓存。
//
// 探测方式是"实际编码一小段测试画面"，而非仅检查编码器列表：
// ffmpeg 普遍编译了 nvenc/vaapi 支持，但设备缺失或驱动库不全时
// 仍会初始化失败，只有真正跑通才说明可用。
//
// 优先级：NVENC > VAAPI > 软件编码。
func ProbeH264Encoder(ffmpegPath string) (EncoderKind, string) {
	hwOnce.Do(func() {
		hwKind, hwDesc = detectEncoder(ffmpegPath)
		log.Printf("[hwaccel] selected encoder: %s (%s)", hwKind, hwDesc)
	})
	return hwKind, hwDesc
}

// H264EncodeArgs 返回指定编码器的转码参数。
//
// 传入 detail 是为了在 VAAPI 场景下取回探测到的设备节点。
func H264EncodeArgs(kind EncoderKind, detail string) []string {
	switch kind {
	case EncoderNVENC:
		// 对齐 libx264 -preset veryfast（CRF 23）的观感：
		// CQ 模式负责质量，maxrate 限制复杂场景下的码率上限，避免无限膨胀。
		return []string{
			"-c:v", "h264_nvenc",
			"-preset", "p4",
			"-tune", "hq",
			"-rc", "vbr",
			"-cq", "23",
			"-b:v", "0",
			"-maxrate", "12M",
			"-bufsize", "24M",
			"-profile:v", "high",
			"-pix_fmt", "yuv420p",
			"-g", "30",
		}
	case EncoderVAAPI:
		dev := hwVAAPID
		if dev == "" {
			dev = "/dev/dri/renderD128"
		}
		return []string{
			"-vaapi_device", dev,
			"-vf", "format=nv12,hwupload",
			"-c:v", "h264_vaapi",
			"-qp", "23",
			"-g", "30",
		}
	default:
		return []string{
			"-c:v", "libx264",
			"-preset", "veryfast",
			"-pix_fmt", "yuv420p",
			"-g", "30",
		}
	}
}

// detectEncoder 探测并选择 H.264 编码器。
//
// 可用 NVR_HW_ENCODER 显式指定：
//
//	auto（默认）  实测对比硬件与软件编码速度，选更快的
//	nvenc         强制 NVIDIA NVENC
//	vaapi         强制 VAAPI
//	software      强制软件编码
//
// 之所以默认做实测而非无条件优先硬件：老一代 NVENC（如 Pascal 架构的
// Tesla P4）在 1080p 下可能明显慢于现代多核 CPU 的软编，盲目启用反而
// 拖慢转码。硬件编码的真正价值在于多路并发时释放 CPU 与不受会话数限制。
func detectEncoder(ffmpegPath string) (EncoderKind, string) {
	forced := strings.ToLower(strings.TrimSpace(os.Getenv("NVR_HW_ENCODER")))

	// 收集可用硬件编码器
	type cand struct {
		kind EncoderKind
		name string
	}
	var avail []cand
	if forced == "" || forced == "auto" || forced == "nvenc" {
		if hasNvidiaDevice() &&
			tryEncode(ffmpegPath, []string{"-c:v", "h264_nvenc", "-preset", "p4", "-f", "null", "-"}) {
			name := "NVIDIA NVENC"
			if d := nvidiaName(); d != "" {
				name += " (" + d + ")"
			}
			avail = append(avail, cand{EncoderNVENC, name})
		}
	}
	if forced == "" || forced == "auto" || forced == "vaapi" {
		for _, dev := range vaapiCandidates() {
			if tryEncode(ffmpegPath, []string{
				"-vaapi_device", dev,
				"-vf", "format=nv12,hwupload",
				"-c:v", "h264_vaapi",
				"-f", "null", "-",
			}) {
				hwVAAPID = dev
				avail = append(avail, cand{EncoderVAAPI, "VAAPI " + dev})
				break
			}
		}
	}

	if forced == "software" {
		return EncoderSoftware, "libx264 (forced)"
	}
	if forced != "" && forced != "auto" {
		if len(avail) > 0 {
			return avail[0].kind, avail[0].name + " (forced)"
		}
		log.Printf("[hwaccel] %q requested but unavailable, using software", forced)
		return EncoderSoftware, "libx264 (requested " + forced + " unavailable)"
	}
	if len(avail) == 0 {
		return EncoderSoftware, "libx264 (no hw encoder)"
	}

	// auto：与软编实测对比，选更快的
	swMs := benchEncode(ffmpegPath, H264EncodeArgs(EncoderSoftware, ""))
	for _, c := range avail {
		hwMs := benchEncode(ffmpegPath, H264EncodeArgs(c.kind, c.name))
		if hwMs > 0 && swMs > 0 && hwMs < swMs {
			return c.kind, fmt.Sprintf("%s, %.0fms vs software %.0fms", c.name, hwMs, swMs)
		}
		log.Printf("[hwaccel] %s slower than software (%.0fms vs %.0fms), skipping", c.name, hwMs, swMs)
	}
	return EncoderSoftware, fmt.Sprintf("libx264 (faster than hw: %.0fms)", swMs)
}

// benchEncode 用固定样本粗略测量编码耗时（毫秒），失败返回 0。
func benchEncode(ffmpegPath string, encArgs []string) float64 {
	args := []string{
		"-hide_banner", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "testsrc2=size=1280x720:rate=25", "-t", "2",
	}
	args = append(args, encArgs...)
	args = append(args, "-f", "null", "-")
	start := time.Now()
	if err := exec.Command(ffmpegPath, args...).Run(); err != nil {
		return 0
	}
	return float64(time.Since(start).Milliseconds())
}

// tryEncode 用极小的合成画面实测编码器能否真正初始化并编码。
func tryEncode(ffmpegPath string, encArgs []string) bool {
	args := []string{
		"-hide_banner", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "testsrc2=size=320x240:rate=5", "-t", "0.2",
	}
	args = append(args, encArgs...)
	cmd := exec.Command(ffmpegPath, args...)
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Run(); err != nil {
		return false
	}
	return true
}

// hasNvidiaDevice 检查容器内是否存在 NVIDIA 设备节点。
func hasNvidiaDevice() bool {
	matches, _ := filepath.Glob("/dev/nvidia[0-9]*")
	return len(matches) > 0
}

// nvidiaName 尽力获取显卡型号，仅用于日志展示。
func nvidiaName() string {
	out, err := exec.Command("nvidia-smi", "--query-gpu=name", "--format=csv,noheader").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(strings.Split(string(out), "\n")[0])
}

// vaapiCandidates 列出可能可用的 VAAPI 渲染节点。
func vaapiCandidates() []string {
	var out []string
	if v := os.Getenv("NVR_VAAPI_DEVICE"); v != "" {
		out = append(out, v)
	}
	matches, _ := filepath.Glob("/dev/dri/renderD*")
	out = append(out, matches...)
	return out
}
