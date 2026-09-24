package ffmpeg

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ============================ 类型定义 ============================

// EncoderKind 表示选用的视频编码器。
type EncoderKind string

const (
	EncoderNVENC    EncoderKind = "h264_nvenc"
	EncoderVAAPI    EncoderKind = "h264_vaapi"
	EncoderSoftware EncoderKind = "libx264"
)

// DecoderKind 表示选用的硬件解码后端。
type DecoderKind string

const (
	DecoderCUDA     DecoderKind = "cuda"
	DecoderVAAPI    DecoderKind = "vaapi"
	DecoderSoftware DecoderKind = "sw"
)

// HWSelection 是探测得到的硬件加速方案。
type HWSelection struct {
	Encoder     EncoderKind
	EncoderDesc string
	Decoder     DecoderKind
	DecoderDesc string
	// Reason 说明如何得到该结果（用于日志与排查）
	Reason string
}

var (
	hwOnce sync.Once
	hwSel  *HWSelection
	vaapiD string
)

// ============================ 对外接口 ============================

// ProbeHW 探测并缓存本机可用的硬件编解码方案。
//
// 配置项（均可选）：
//
//	NVR_HWACCEL=auto|off                        总开关，默认 auto（启用硬件加速）
//	NVR_ENCODER_PRIORITY=nvenc,vaapi,software   编码器优先级，默认硬编优先
//	NVR_DECODER_PRIORITY=cuda,vaapi,software    解码器优先级，默认硬解优先
//	NVR_VAAPI_DEVICE=/dev/dri/renderD128        指定 VAAPI 设备，默认自动探测
//
// 探测方式是实际编解码一小段测试画面：ffmpeg 普遍编译了 nvenc/vaapi 支持，
// 设备缺失或驱动库不全时仍会初始化失败，只有真正跑通才算可用。
func ProbeHW(ffmpegPath string) *HWSelection {
	hwOnce.Do(func() {
		hwSel = detect(ffmpegPath)
		log.Printf("[hwaccel] encoder=%s (%s) | decoder=%s (%s)",
			hwSel.Encoder, hwSel.EncoderDesc, hwSel.Decoder, hwSel.DecoderDesc)
		if hwSel.Reason != "" {
			log.Printf("[hwaccel] %s", hwSel.Reason)
		}
	})
	return hwSel
}

// EncodeArgs 返回编码参数。
func (h *HWSelection) EncodeArgs() []string { return H264EncodeArgs(h.Encoder, "") }

// DecodeArgs 返回解码参数（需置于 -i 之前）；软件解码返回 nil。
func (h *HWSelection) DecodeArgs() []string { return DecodeArgs(h.Decoder) }

// DecodeArgs 按解码后端返回 ffmpeg 参数（放在 -i 之前）。
func DecodeArgs(kind DecoderKind) []string {
	switch kind {
	case DecoderCUDA:
		// NVDEC：让 ffmpeg 按比特流自动选择对应 cuvid 解码器
		return []string{"-hwaccel", "cuda"}
	case DecoderVAAPI:
		dev := vaapiD
		if dev == "" {
			dev = "/dev/dri/renderD128"
		}
		return []string{"-hwaccel", "vaapi", "-hwaccel_device", dev}
	default:
		return nil
	}
}

// H264EncodeArgs 返回指定编码器的转码参数，目标是贴合
// libx264 -preset veryfast（CRF 23）的观感。
func H264EncodeArgs(kind EncoderKind, _ string) []string {
	switch kind {
	case EncoderNVENC:
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
		return []string{
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

// ============================ 探测实现 ============================

func detect(ffmpegPath string) *HWSelection {
	sel := &HWSelection{}

	if !hwaccelEnabled() {
		sel.Encoder, sel.EncoderDesc = EncoderSoftware, "libx264"
		sel.Decoder, sel.DecoderDesc = DecoderSoftware, "software"
		sel.Reason = "硬件加速已由 NVR_HWACCEL=off 关闭"
		return sel
	}

	var notes []string

	// ---- 编码器：候选按优先级排列，逐个探测可用性 ----
	encPriority := parsePriority(os.Getenv("NVR_ENCODER_PRIORITY"),
		[]string{"nvenc", "vaapi", "software"})
	var encCands []encCandidate
	for _, name := range encPriority {
		kind, desc, ok := probeEncoder(ffmpegPath, name)
		if ok {
			encCands = append(encCands, encCandidate{kind, desc})
			if kind == EncoderSoftware {
				break // 软编始终可用，作为兜底候选即可
			}
		} else if name != "software" {
			notes = append(notes, name+"编码不可用")
		}
	}
	if len(encCands) == 0 {
		encCands = append(encCands, encCandidate{EncoderSoftware, "libx264 (兜底)"})
		notes = append(notes, "无可用硬件编码器")
	}

	// 单一候选时无需基准测试
	if len(encCands) == 1 {
		sel.Encoder, sel.EncoderDesc = encCands[0].kind, encCands[0].desc
	} else {
		// 多候选：实测综合评分挑选最优（吞吐达标前提下优先省 CPU）
		minRT := minThroughput()
		best, report := pickBest(ffmpegPath, encCands, minRT)
		sel.Encoder, sel.EncoderDesc = best.kind, best.desc
		sel.Reason = report
	}

	// ---- 解码器：按优先级取首个可用 ----
	decPriority := parsePriority(os.Getenv("NVR_DECODER_PRIORITY"),
		[]string{"cuda", "vaapi", "software"})
	decFound := false
	for _, name := range decPriority {
		kind, desc, ok := probeDecoder(ffmpegPath, name)
		if ok {
			sel.Decoder, sel.DecoderDesc = kind, desc
			decFound = true
			break
		}
		if name != "software" {
			notes = append(notes, name+"解码不可用")
		}
	}
	if !decFound {
		sel.Decoder, sel.DecoderDesc = DecoderSoftware, "software (兜底)"
	}

	prio := fmt.Sprintf("优先级 编码[%s] 解码[%s]",
		strings.Join(encPriority, ">"), strings.Join(decPriority, ">"))
	if sel.Reason == "" {
		sel.Reason = prio
	} else {
		sel.Reason = prio + "；" + sel.Reason
	}
	if len(notes) > 0 {
		sel.Reason += "；" + strings.Join(notes, "，")
	}
	return sel
}

// encCandidate 是参与评分的编码器候选。
type encCandidate struct {
	kind EncoderKind
	desc string
}

// pickBest 对候选编码器做基准测试，返回最优者与可读的对比报告。
//
// 评分原则（NVR 场景）：
//  1. 吞吐必须达标（≥ NVR_MIN_THROUGHPUT 倍实时，默认 1.2x），否则转码跟不上录像
//  2. 达标者中优先选择 CPU 占用最低的 —— CPU 是 NVR 的稀缺资源
//     （还需承载 AI 检测、快照、预览等）
//  3. 若全部不达标，退而选吞吐最高者，并在日志中说明
func pickBest(ffmpegPath string, cands []encCandidate, minRT float64) (encCandidate, string) {
	results := make([]benchResult, 0, len(cands))
	for _, c := range cands {
		r := benchEncoder(ffmpegPath, c.kind, c.desc)
		results = append(results, r)
		if r.wallMs > 0 {
			log.Printf("[hwaccel] 基准 %-28s 吞吐 %.1fx实时  CPU %.0f%%",
				r.desc, r.realtime, r.cpuPct)
		}
	}

	// 1) 达标者中选 CPU 最低
	best := -1
	for i, r := range results {
		if r.wallMs <= 0 || r.realtime < minRT {
			continue
		}
		if best < 0 || r.cpuPct < results[best].cpuPct {
			best = i
		}
	}
	if best >= 0 {
		return cands[best], fmt.Sprintf(
			"实测择优：%s（吞吐 %.1fx 实时、CPU %.0f%%），阈值 %.1fx",
			results[best].desc, results[best].realtime, results[best].cpuPct, minRT)
	}

	// 2) 都不达标 → 取吞吐最高
	fastest := 0
	for i, r := range results {
		if r.wallMs > 0 && (results[fastest].wallMs <= 0 || r.realtime > results[fastest].realtime) {
			fastest = i
		}
	}
	if results[fastest].wallMs <= 0 {
		return cands[0], "基准测试均失败，沿用优先级首位"
	}
	return cands[fastest], fmt.Sprintf(
		"无候选达到 %.1fx 实时阈值，改用吞吐最高的 %s（%.1fx）",
		minRT, results[fastest].desc, results[fastest].realtime)
}

// benchResult 是一次编码基准测试的结果。
type benchResult struct {
	desc     string
	wallMs   float64 // 墙钟耗时
	cpuMs    float64 // 子进程 CPU 时间（user+sys）
	realtime float64 // 吞吐倍率（相对实时）
	cpuPct   float64 // CPU 占用百分比（cpuMs/wallMs*100）
}

// benchEncoder 用固定样本测量编码器的吞吐与 CPU 占用。
func benchEncoder(ffmpegPath string, kind EncoderKind, desc string) benchResult {
	const secs = 3.0 // 样本时长
	args := []string{
		"-hide_banner", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", fmt.Sprintf("testsrc2=size=1280x720:rate=25"),
		"-t", fmt.Sprintf("%.0f", secs),
	}
	args = append(args, H264EncodeArgs(kind, "")...)
	args = append(args, "-f", "null", "-")

	cmd := exec.Command(ffmpegPath, args...)
	start := time.Now()
	err := cmd.Run()
	wall := time.Since(start)

	r := benchResult{desc: desc}
	if err != nil {
		return r
	}
	r.wallMs = float64(wall.Milliseconds())
	cpu := float64((cmd.ProcessState.UserTime() + cmd.ProcessState.SystemTime()) / time.Millisecond)
	r.cpuMs = cpu
	if r.wallMs > 0 {
		r.realtime = secs / (r.wallMs / 1000.0)
		r.cpuPct = cpu / r.wallMs * 100
	}
	return r
}

// minThroughput 返回可接受的最低吞吐倍率（相对实时）。
func minThroughput() float64 {
	if v := strings.TrimSpace(os.Getenv("NVR_MIN_THROUGHPUT")); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f > 0 {
			return f
		}
	}
	return 1.2
}

// probeEncoder 实测某个编码器是否可用。
func probeEncoder(ffmpegPath, name string) (EncoderKind, string, bool) {
	switch strings.ToLower(name) {
	case "nvenc", "h264_nvenc":
		if !hasNvidiaDevice() {
			return "", "", false
		}
		if tryEncode(ffmpegPath, []string{"-c:v", "h264_nvenc", "-preset", "p4", "-f", "null", "-"}) {
			desc := "NVIDIA NVENC"
			if n := nvidiaName(); n != "" {
				desc += " (" + n + ")"
			}
			return EncoderNVENC, desc, true
		}
	case "vaapi", "h264_vaapi":
		for _, dev := range vaapiCandidates() {
			if tryEncode(ffmpegPath, []string{
				"-vaapi_device", dev,
				"-vf", "format=nv12,hwupload",
				"-c:v", "h264_vaapi",
				"-f", "null", "-",
			}) {
				vaapiD = dev
				return EncoderVAAPI, "VAAPI " + dev, true
			}
		}
	case "software", "libx264":
		return EncoderSoftware, "libx264 (software)", true
	}
	return "", "", false
}

// probeDecoder 实测某个解码后端是否可用。
//
// 合成测试源不经过解码器，因此先生成一个极小 H.264 样片再解码，
// 确保结论真实可靠。
func probeDecoder(ffmpegPath, name string) (DecoderKind, string, bool) {
	switch strings.ToLower(name) {
	case "cuda", "nvdec", "cuvid":
		if !hasNvidiaDevice() {
			return "", "", false
		}
		if tryDecode(ffmpegPath, []string{"-hwaccel", "cuda"}) {
			desc := "NVDEC (CUDA)"
			if n := nvidiaName(); n != "" {
				desc += " " + n
			}
			return DecoderCUDA, desc, true
		}
	case "vaapi":
		for _, dev := range vaapiCandidates() {
			if tryDecode(ffmpegPath, []string{"-hwaccel", "vaapi", "-hwaccel_device", dev}) {
				vaapiD = dev
				return DecoderVAAPI, "VAAPI " + dev, true
			}
		}
	case "software", "sw":
		return DecoderSoftware, "software", true
	}
	return "", "", false
}

// tryEncode 用合成画面实测编码器能否真正初始化并编码。
func tryEncode(ffmpegPath string, encArgs []string) bool {
	args := []string{
		"-hide_banner", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "testsrc2=size=320x240:rate=5", "-t", "0.2",
	}
	args = append(args, encArgs...)
	return exec.Command(ffmpegPath, args...).Run() == nil
}

// tryDecode 生成极小 H.264 样片并用指定后端解码，验证硬解真实可用。
func tryDecode(ffmpegPath string, decArgs []string) bool {
	dir, err := os.MkdirTemp("", "hwprobe")
	if err != nil {
		return false
	}
	defer os.RemoveAll(dir)
	sample := filepath.Join(dir, "s.mp4")

	mk := []string{
		"-hide_banner", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "testsrc2=size=320x240:rate=10", "-t", "0.5",
		"-c:v", "libx264", "-preset", "ultrafast", "-pix_fmt", "yuv420p", sample,
	}
	if exec.Command(ffmpegPath, mk...).Run() != nil {
		return false
	}
	if _, err := os.Stat(sample); err != nil {
		return false
	}

	args := []string{"-hide_banner", "-loglevel", "error", "-y"}
	args = append(args, decArgs...)
	args = append(args, "-i", sample, "-f", "null", "-")

	out, err := exec.Command(ffmpegPath, args...).CombinedOutput()
	if err != nil {
		if s := string(out); s != "" {
			log.Printf("[hwaccel] 解码探测失败 %s: %s", strings.Join(decArgs, " "), firstLine(s))
		}
		return false
	}
	return true
}

// ============================ 辅助函数 ============================

// hwaccelEnabled 硬件加速总开关（默认开启）。
func hwaccelEnabled() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("NVR_HWACCEL")))
	return v != "off" && v != "false" && v != "0" && v != "disable"
}

// parsePriority 解析逗号分隔的优先级列表，空值回退默认。
func parsePriority(env string, def []string) []string {
	if strings.TrimSpace(env) == "" {
		return def
	}
	var out []string
	for _, p := range strings.Split(env, ",") {
		if p = strings.TrimSpace(strings.ToLower(p)); p != "" {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return def
	}
	return out
}

func hasNvidiaDevice() bool {
	m, _ := filepath.Glob("/dev/nvidia[0-9]*")
	return len(m) > 0
}

func nvidiaName() string {
	out, err := exec.Command("nvidia-smi", "--query-gpu=name", "--format=csv,noheader").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(strings.Split(string(out), "\n")[0])
}

func vaapiCandidates() []string {
	var out []string
	if v := os.Getenv("NVR_VAAPI_DEVICE"); v != "" {
		out = append(out, v)
	}
	m, _ := filepath.Glob("/dev/dri/renderD*")
	return append(out, m...)
}
