package recorder

import (
	"bytes"
	"fmt"
	"log"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"cyannvr/server/config"
	"cyannvr/server/models"
	"cyannvr/server/pkg/ai"
	"cyannvr/server/pkg/ffmpeg"
	"cyannvr/server/store"
)

type Manager struct {
	cfg     *config.Config
	st      *store.Store
	ai      *ai.Analyzer
	onEvent func(eventType, deviceID, deviceName, eventID, label, desc, time string)

	mu      sync.Mutex
	workers map[string]*Worker
	stopAll chan struct{}
	once    sync.Once

	ShouldRecordFn func() (mode, scheduleStart, scheduleEnd string)
}

type Worker struct {
	mgr *Manager
	dev *models.Device

	recordProc *ffmpeg.Proc
	liveProc   *ffmpeg.Proc
	snapProc   *ffmpeg.Proc
	// teeProc 是单连接模式下的主进程：一个 RTSP 连接同时输出录像分段与 HLS。
	// 部分低端摄像头仅允许单个活跃 RTSP 会话，多连接会导致全部失败。
	teeProc *ffmpeg.Proc

	// singleConn 表示当前是否使用单连接模式
	singleConn bool
	// streamErrs 记录连续出现的流读取错误次数，用于自动降级判断
	streamErrs int
	// diskPaused 磁盘低水位暂停标记（由 supervise 维护，用于只打一次日志）
	diskPaused bool
	// authFails 认证失败（401）连续次数，驱动 fail() 里的长退避档位递增
	authFails int
	// lastAuthErrAt 最近一次「401 认证失败」发生的时刻；fail() 依据它与
	// authFailureWindow 判断是否走长退避（比较经过时间，不是比较截止时刻）。
	lastAuthErrAt time.Time
	// stall 假死看门狗的每进程采样状态（键为角色名 record/live/tee）
	stall map[string]*stallState
	// 日志降噪状态：同类告警键、上次输出时刻、被省略次数
	errKey   string
	errAt    time.Time
	errCount int

	recordDir string
	liveDir   string
	snapDir   string

	restarts int
	stop     chan struct{}
}

func NewManager(cfg *config.Config, st *store.Store, a *ai.Analyzer, onEvent func(string, string, string, string, string, string, string)) *Manager {
	m := &Manager{cfg: cfg, st: st, ai: a, onEvent: onEvent, workers: map[string]*Worker{}, stopAll: make(chan struct{})}
	for _, d := range []string{cfg.RecordDir, cfg.LiveDir, cfg.SnapDir, cfg.EventDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			log.Printf("mkdir %s: %v", d, err)
		}
	}
	return m
}

func (m *Manager) Start() {
	if m.ai != nil {
		go m.aiLoop()
	}
	go m.indexerLoop()
	go m.cleanupLoop()
	go m.retryLoop()
	devs, err := m.st.ListDevices()
	if err != nil {
		log.Printf("list devices: %v", err)
		return
	}
	for _, d := range devs {
		if d.Online {
			m.StartWorker(&d)
		}
	}
}

func (m *Manager) Stop() {
	m.once.Do(func() { close(m.stopAll) })
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, w := range m.workers {
		w.Stop()
	}
}

func (m *Manager) StartWorker(d *models.Device) {
	if !ffmpeg.Exists(m.cfg.Ffmpeg) {
		log.Printf("ffmpeg not found (%s), cannot start recorder", m.cfg.Ffmpeg)
		return
	}
	m.mu.Lock()
	if _, ok := m.workers[d.ID]; ok {
		m.mu.Unlock()
		return
	}
	w := &Worker{
		mgr:       m,
		dev:       d,
		recordDir: filepath.Join(m.cfg.RecordDir, d.ID),
		liveDir:   filepath.Join(m.cfg.LiveDir, d.ID),
		snapDir:   filepath.Join(m.cfg.SnapDir, d.ID),
		stop:      make(chan struct{}),
	}
	m.workers[d.ID] = w
	m.mu.Unlock()

	_ = os.MkdirAll(w.recordDir, 0o755)
	_ = os.MkdirAll(w.liveDir, 0o755)
	_ = os.MkdirAll(w.snapDir, 0o755)
	if m.ai != nil {
		m.ai.Register(d.ID)
	}
	go w.supervise()
}

func (m *Manager) StopWorker(id string) {
	m.mu.Lock()
	w, ok := m.workers[id]
	delete(m.workers, id)
	m.mu.Unlock()
	if ok {
		w.Stop()
	}
}

func (m *Manager) Restart(d *models.Device) {
	m.StopWorker(d.ID)
	m.StartWorker(d)
}

func (m *Manager) removeWorker(id string) {
	m.mu.Lock()
	delete(m.workers, id)
	m.mu.Unlock()
}

func (m *Manager) IsRunning(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.workers[id]
	return ok
}

// ProcStats 汇总当前 ffmpeg 进程概况，供 /api/status 展示。
//
// 回答运维最关心的两个问题：现在总共起了几个 ffmpeg（有无泄漏）、
// 每个 worker 各持有几个。
//
// 按 PID 去重：单连接模式下 recordProc 与 teeProc 指向同一个进程
// （tee 一个进程同时产出录像分段与 HLS），逐字段计数会把它数两次，
// 让「ffmpeg 进程数」虚高，反而掩盖真实泄漏。
func (m *Manager) ProcStats() map[string]any {
	m.mu.Lock()
	defer m.mu.Unlock()
	total := 0
	perDevice := make(map[string]int, len(m.workers))
	for id, w := range m.workers {
		seen := map[int]bool{}
		for _, p := range []*ffmpeg.Proc{w.recordProc, w.liveProc, w.snapProc, w.teeProc} {
			if p == nil || !p.Running() {
				continue
			}
			pid := p.Pid()
			if pid > 0 {
				if seen[pid] {
					continue // 同一进程的另一个字段别名，跳过
				}
				seen[pid] = true
				continue
			}
			// 拿不到 PID（进程刚启动/已退出）时退回按字段计数，
			// 用负键避免与真实 PID 冲突
			seen[-(len(seen) + 1)] = true
		}
		perDevice[id] = len(seen)
		total += len(seen)
	}
	return map[string]any{
		"total":    total,
		"workers":  len(m.workers),
		"byDevice": perDevice,
	}
}

// ---------- worker ----------

func (w *Worker) inputArgs() []string {
	if w.dev.Source == models.SourceTest {
		return []string{"-f", "lavfi", "-i", "testsrc2=size=640x360:rate=15"}
	}
	url := w.inputURL()
	// 硬件解码参数需置于 -i 之前
	args := []string{"-rtsp_transport", "tcp"}
	args = append(args, ffmpeg.ProbeHW(w.mgr.cfg.Ffmpeg).DecodeArgs()...)
	args = append(args, "-i", url)
	return args
}

// streamURL resolves the RTSP URL for a given role ("preview" or "record"),
// preferring the stream the user selected; falls back to the device RTSPURL,
// then the constructed default URL. Credentials are NOT embedded here.
func (w *Worker) streamURL(role string) string {
	if w.dev.Source == models.SourceTest {
		return "lavfi"
	}
	sel := w.dev.PreviewStream
	if role == "record" {
		sel = w.dev.RecordStream
	}
	if sel != "" {
		for _, s := range w.dev.Streams {
			if s.ID == sel && s.URL != "" {
				return s.URL
			}
		}
	}
	raw := w.dev.RTSPURL
	if raw == "" {
		u := url.URL{Scheme: "rtsp", Host: net.JoinHostPort(w.dev.IP, strconv.Itoa(w.dev.Port)), Path: "/stream1"}
		return u.String()
	}
	return raw
}

// withCreds embeds RTSP credentials into the URL userinfo, properly escaped.
func (w *Worker) withCreds(rawURL string) string {
	if w.dev.Username == "" || strings.Contains(rawURL, "@") {
		return rawURL
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	u.User = url.UserPassword(w.dev.Username, w.dev.Password)
	return u.String()
}

func (w *Worker) inputURL() string {
	return w.withCreds(w.streamURL("preview"))
}

func (w *Worker) recordInputArgs() []string {
	if w.dev.Source == models.SourceTest {
		return []string{"-f", "lavfi", "-i", "testsrc2=size=640x360:rate=15"}
	}
	args := []string{"-rtsp_transport", "tcp"}
	args = append(args, ffmpeg.ProbeHW(w.mgr.cfg.Ffmpeg).DecodeArgs()...)
	args = append(args, "-i", w.withCreds(w.streamURL("record")))
	return args
}

func transcodeArgs(ffmpegPath string) []string {
	// 与回放转码保持一致：按配置优先级选择编码器
	return ffmpeg.ProbeHW(ffmpegPath).EncodeArgs()
}

// useSingleConnection 判断是否采用单连接模式。
//
//	NVR_SINGLE_CONNECTION=on   强制单连接（tee 同时输出录像与 HLS）
//	NVR_SINGLE_CONNECTION=off  强制多连接（各自独立进程）
//	其它（默认 auto）          由运行时自动降级决定
func (w *Worker) useSingleConnection() bool {
	// 1) 设备自身记录的模式最优先（可能是此前自动探测并持久化的结果）
	switch strings.ToLower(strings.TrimSpace(w.dev.ConnMode)) {
	case "single":
		return true
	case "multi":
		return false
	}
	// 2) 其次看全局强制开关
	switch strings.ToLower(strings.TrimSpace(os.Getenv("NVR_SINGLE_CONNECTION"))) {
	case "on", "true", "1", "yes":
		return true
	case "off", "false", "0", "no":
		return false
	}
	// 3) 默认 auto：由运行时自动降级决定
	return w.singleConn
}

// markStreamError 记录一次流读取失败；连续失败达阈值时自动降级为单连接模式。
//
// 触发场景：摄像头只允许一个活跃 RTSP 会话时，多连接会互相挤占，
// 表现为 "Invalid data found when processing input" 并陷入重启循环。
func (w *Worker) markStreamError(reason string) {
	if !strings.Contains(reason, "Invalid data") && !strings.Contains(reason, "Invalid data found") {
		return
	}
	w.streamErrs++
	if w.streamErrs >= 2 && !w.singleConn {
		w.singleConn = true
		log.Printf("[%s] 连续 %d 次流读取失败，自动切换为单连接模式（tee 合并录像与 HLS）",
			w.dev.Name, w.streamErrs)
		// 把探测结果写回设备，避免每次重启都重新试错
		w.dev.ConnMode = "single"
		if err := w.mgr.st.UpdateDevice(*w.dev); err != nil {
			log.Printf("[%s] 持久化连接模式失败: %v", w.dev.Name, err)
		} else {
			log.Printf("[%s] 已将连接模式记录为 single", w.dev.Name)
		}
	}
}

// startSingleConn 用单个 RTSP 连接同时输出录像分段与 HLS，
// 快照改从 HLS 播放列表抓帧，从而把连接数从 3 降到 1。
func (w *Worker) startSingleConn() error {
	in := w.inputArgs() // 已含硬件解码参数
	codec := w.pickCodec()

	for _, off := range []int{0, 1, 2} {
		day := time.Now().AddDate(0, 0, off).Format("20060102")
		_ = os.MkdirAll(filepath.Join(w.recordDir, day), 0o755)
	}

	recTpl := filepath.Join(w.recordDir, "%Y%m%d", "%H%M%S.mp4")
	livePl := filepath.Join(w.liveDir, "index.m3u8")
	// segment_format_options 把 +faststart 透传给 mp4 muxer（索引前置，回放秒开）
	// 直播窗口 8 片（hls_time=2，约 16 秒）：客户端切后台或网络抖动后，仍有
	// 足够窗口重新拉到未删除的分片，减少 404 断流黑屏；代价是每路直播多占
	// 几 MB 磁盘。此前 4 片（约 8 秒）实测过窄。
	teeSpec := fmt.Sprintf(
		"[f=segment:segment_time=300:reset_timestamps=1:strftime=1:segment_format_options=movflags=+faststart]%s"+
			"|[f=hls:hls_time=2:hls_list_size=8:hls_flags=delete_segments]%s",
		recTpl, livePl)

	// -map 0:v 是必需的：缺少显式流映射时 tee 会报
	// "Output file #0 does not contain any stream" 而无法输出
	args := append(append([]string{}, in...), "-map", "0:v", "-an")
	args = append(args, codec...)
	args = append(args, "-f", "tee", teeSpec)

	log.Printf("[%s] starting ffmpeg (single-conn): %v", w.dev.Name, sanitizeArgs(args))
	var err error
	if w.teeProc, err = ffmpeg.Start(w.mgr.cfg.Ffmpeg, args...); err != nil {
		log.Printf("[%s] single-conn start error: %v", w.dev.Name, err)
		return err
	}

	// 快照从 HLS 播放列表读取：不新增 RTSP 连接。
	// AI 关闭时不启动（current.jpg 只服务 AI 分析）。
	if w.needSnapshots() {
		snapArgs := append(w.snapInputArgs(),
			"-vf", "fps=1", "-update", "1", filepath.Join(w.snapDir, "current.jpg"))
		w.snapProc, _ = ffmpeg.Start(w.mgr.cfg.Ffmpeg, snapArgs...)
	}

	// 复用字段便于既有逻辑（waitExit / killProcs）统一处理
	w.recordProc = w.teeProc
	w.liveProc = nil
	return nil
}

// pickCodec 返回编码参数：h264 源直接 copy，否则转码（按硬件优先级）。
func (w *Worker) pickCodec() []string {
	if w.dev.Source == models.SourceTest {
		return transcodeArgs(w.mgr.cfg.Ffmpeg)
	}
	for _, url := range []string{w.streamURL("preview"), w.streamURL("record")} {
		if c := ffmpeg.ProbeVideoCodec(w.mgr.cfg.Ffmpeg, url); c != "" && c != "h264" {
			log.Printf("[%s] input codec=%s, transcoding to h264 for browser compatibility", w.dev.Name, c)
			return transcodeArgs(w.mgr.cfg.Ffmpeg)
		}
	}
	return []string{"-c", "copy"}
}

func (w *Worker) startProcs() error {
	if w.useSingleConnection() {
		return w.startSingleConn()
	}
	return w.startMultiConn()
}

func (w *Worker) startMultiConn() error {
	in := w.inputArgs()
	recIn := w.recordInputArgs()
	transcode := w.dev.Source == models.SourceTest
	if !transcode && w.dev.Source != models.SourceTest {
		// Transcode if either the preview or the record stream is not h264.
		for _, url := range []string{w.streamURL("preview"), w.streamURL("record")} {
			codec := ffmpeg.ProbeVideoCodec(w.mgr.cfg.Ffmpeg, url)
			if codec != "" && codec != "h264" {
				log.Printf("[%s] input codec=%s, transcoding to h264 for browser compatibility", w.dev.Name, codec)
				transcode = true
				break
			}
		}
	}
	// Ensure the per-day recording subdirectory exists; ffmpeg's segment
	// muxer cannot create nested directories itself. Pre-create today and
	// tomorrow so segments never fail across midnight.
	for _, off := range []int{0, 1, 2} {
		day := time.Now().AddDate(0, 0, off).Format("20060102")
		if err := os.MkdirAll(filepath.Join(w.recordDir, day), 0o755); err != nil {
			log.Printf("[%s] mkdir record day %s: %v", w.dev.Name, day, err)
		}
	}
	codec := []string{"-c", "copy"}
	if transcode {
		codec = transcodeArgs(w.mgr.cfg.Ffmpeg)
	}

	recArgs := append(append([]string{}, recIn...), "-an")
	recArgs = append(recArgs, codec...)
	// -movflags +faststart：把 moov 索引前置，播放器无需下载整个文件
	// 就能起播（分段录像只有几十 MB，代价极小；对网页回放的首帧等待
	// 与「半截文件」识别都有帮助）。
	// segment 复用器需通过 segment_format_options 把该参数透传给 mp4 muxer。
	recArgs = append(recArgs,
		"-f", "segment", "-segment_time", "300", "-reset_timestamps", "1", "-strftime", "1",
		"-segment_format_options", "movflags=+faststart",
		filepath.Join(w.recordDir, "%Y%m%d", "%H%M%S.mp4"))

	liveArgs := append(append([]string{}, in...), "-an")
	if transcode {
		liveArgs = append(liveArgs, transcodeArgs(w.mgr.cfg.Ffmpeg)...)
	} else {
		liveArgs = append(liveArgs, "-c:v", "copy")
	}
	// 直播窗口与 tee 模式一致：8 片约 16 秒，降低切后台后 404 断流黑屏概率。
	liveArgs = append(liveArgs,
		"-f", "hls", "-hls_time", "2", "-hls_list_size", "8", "-hls_flags", "delete_segments",
		filepath.Join(w.liveDir, "index.m3u8"))

	log.Printf("[%s] starting ffmpeg: record=%v", w.dev.Name, sanitizeArgs(recArgs))
	var err error
	if w.recordProc, err = ffmpeg.Start(w.mgr.cfg.Ffmpeg, recArgs...); err != nil {
		log.Printf("[%s] record start error: %v", w.dev.Name, err)
		return err
	}
	log.Printf("[%s] starting ffmpeg: live=%v", w.dev.Name, sanitizeArgs(liveArgs))
	if w.liveProc, err = ffmpeg.Start(w.mgr.cfg.Ffmpeg, liveArgs...); err != nil {
		log.Printf("[%s] live start error: %v", w.dev.Name, err)
		w.recordProc.Kill()
		return err
	}
	// 快照：优先从 HLS 读（零额外摄像头连接）；AI 关闭时完全不启动
	if w.needSnapshots() {
		snapArgs := append(w.snapInputArgs(),
			"-vf", "fps=1", "-update", "1", "-y", filepath.Join(w.snapDir, "current.jpg"))
		log.Printf("[%s] starting ffmpeg: snap=%v", w.dev.Name, sanitizeArgs(snapArgs))
		w.snapProc, _ = ffmpeg.Start(w.mgr.cfg.Ffmpeg, snapArgs...)
		if w.snapProc == nil {
			log.Printf("[%s] snap start error (non-fatal)", w.dev.Name)
		}
	}
	return nil
}

// sanitizeArgs masks credentials in RTSP URLs before logging.
func sanitizeArgs(args []string) []string {
	out := make([]string, len(args))
	for i, a := range args {
		if u, err := url.Parse(a); err == nil && u.User != nil && u.User.Username() != "" {
			u.User = url.UserPassword(u.User.Username(), "****")
			out[i] = u.String()
			continue
		}
		if strings.HasPrefix(a, "rtsp://") || strings.HasPrefix(a, "http://") {
			out[i] = redactURL(a)
			continue
		}
		out[i] = a
	}
	return out
}

func redactURL(raw string) string {
	i := strings.Index(raw, "://")
	j := strings.Index(raw[i+3:], "@")
	if i >= 0 && j >= 0 {
		head := raw[:i+3]
		tail := raw[i+3+j:]
		mid := raw[i+3 : i+3+j]
		if c := strings.Index(mid, ":"); c >= 0 {
			return head + mid[:c] + ":****" + tail
		}
	}
	return raw
}

// credPattern 匹配 URL 中的明文凭证（scheme://user:pass@host）。
var credPattern = regexp.MustCompile(`(?i)\b(rtsps?|https?)://([^:@/\s]+):([^@/\s]+)@`)

// redactText 脱敏任意文本里出现的 URL 凭证。
//
// 为什么需要它：ffmpeg 会把输入 URL 原样回显到 stderr，例如
// "rtsp://admin:secret@192.168.1.10:554/stream1: Invalid data found when processing input"。
// 这些 stderr 会被写进容器日志，等于把摄像头密码明文落盘，
// 因此所有外部进程输出都必须先经过这里再记录。
func redactText(s string) string {
	if s == "" {
		return s
	}
	return credPattern.ReplaceAllString(s, "$1://$2:****@")
}

func (w *Worker) killProcs() {
	for _, p := range []*ffmpeg.Proc{w.recordProc, w.liveProc, w.snapProc, w.teeProc} {
		if p != nil {
			p.Kill()
		}
	}
	w.teeProc = nil
}

// ---------- 日志降噪 ----------
//
// 相机离线/密码错时会陷入「启动 ffmpeg → 立刻失败 → 再启动」的循环，
// 每次都把 ffmpeg 的完整 banner（版本、编译参数、各库版本、rtsp 报错）
// 打进日志，一轮几十行。实测 401 场景下 60 秒可刷出上千行、日志涨到 68MB。
//
// 这里做两件事：
//  1. 提炼：只保留真正有诊断价值的行（错误/警告/Stream 信息），丢掉 banner；
//  2. 节流：同类告警（归一化后相同）在 window 内只输出一次，并给出省略计数。
const (
	errThrottleWindow = 30 * time.Second
	errThrottleMaxLen = 300
)

// noiseLinePrefixes 是 ffmpeg banner 里没有诊断价值的前缀，
// 这些行不参与输出也不参与「同类」判定。
var noiseLinePrefixes = []string{
	"ffmpeg version",
	"  built with",
	"  configuration:",
	"  libav",
	"  libavutil",
	"  libavcodec",
	"  libavformat",
	"  libavdevice",
	"  libavfilter",
	"  libswscale",
	"  libswresample",
	"  libpostproc",
	"Press [q] to stop",
	"  Metadata:",
	"    encoder",
}

// distillStderr 从 ffmpeg stderr 中提炼有诊断价值的行。
// 返回空串表示整段都是 banner/噪音，无需记录。
func distillStderr(raw string) string {
	raw = redactText(raw)
	var keep []string
	for _, line := range strings.Split(raw, "\n") {
		t := strings.TrimRight(line, " \t\r")
		if strings.TrimSpace(t) == "" {
			continue
		}
		noisy := false
		for _, p := range noiseLinePrefixes {
			if strings.HasPrefix(t, p) {
				noisy = true
				break
			}
		}
		if noisy {
			continue
		}
		keep = append(keep, t)
	}
	if len(keep) == 0 {
		return ""
	}
	out := strings.Join(keep, " | ")
	if len(out) > errThrottleMaxLen {
		out = out[:errThrottleMaxLen] + "…"
	}
	return out
}

// normErrKey 把一行错误归一化成「同类」键：
// 抹掉指针地址（0x...）与所有数字，这样「同一原因但端口/计数不同」的
// 告警会归为同一类，从而被节流。
var (
	hexPtrRe = regexp.MustCompile(`0x[0-9a-fA-F]+`)
	digitsRe = regexp.MustCompile(`\d+`)
)

func normErrKey(s string) string {
	s = hexPtrRe.ReplaceAllString(s, "#")
	s = digitsRe.ReplaceAllString(s, "#")
	if len(s) > 160 {
		s = s[:160]
	}
	return s
}

// logStderrThrottled 输出一条降噪后的 stderr 告警。
// 同类告警在 errThrottleWindow 内只打第一条，恢复时补一条「共 N 次」。
func (w *Worker) logStderrThrottled(tag, raw string) {
	msg := distillStderr(raw)
	if msg == "" {
		return
	}
	key := normErrKey(msg)
	now := time.Now()

	if w.errKey == key && now.Sub(w.errAt) < errThrottleWindow {
		w.errCount++
		return
	}
	// 上一类告警被压制过，补一条汇总，避免「消失得莫名其妙」
	if w.errKey != "" && w.errCount > 1 {
		log.Printf("[%s] （上一条同类告警共出现 %d 次，已省略）", tag, w.errCount)
	}
	if w.errKey == key {
		log.Printf("[%s] 同类告警持续中（近 %v 内又 %d 次），最新一条：%s",
			tag, errThrottleWindow, w.errCount+1, msg)
	} else {
		log.Printf("[%s] %s", tag, msg)
	}
	w.errKey = key
	w.errAt = now
	w.errCount = 1
}

// killRecordProc stops only the recording process; live HLS and snapshots keep
// running so live viewing and AI motion analysis continue outside the schedule.
func (w *Worker) killRecordProc() {
	if w.recordProc != nil {
		w.recordProc.Kill()
		w.recordProc = nil
	}
}

func (w *Worker) Stop() {
	select {
	case <-w.stop:
	default:
		close(w.stop)
	}
	w.killProcs()
}

func (w *Worker) supervise() {
	defer func() {
		w.mgr.removeWorker(w.dev.ID)
		w.mgr.st.SetDeviceOnline(w.dev.ID, false)
		w.mgr.broadcastEvent("offline", w.dev.ID, w.dev.Name, "", "设备离线", w.dev.Name+" 流已断开", time.Now().Format(time.RFC3339))
	}()
	backoff := time.Second
	for {
		select {
		case <-w.stop:
			return
		default:
		}
		if !w.shouldRecordNow() {
			w.killRecordProc()
			select {
			case <-w.stop:
				w.killProcs()
				return
			case <-time.After(10 * time.Second):
			}
			continue
		}
		// 磁盘低水位：暂停录像，等清理腾出空间后自动恢复。
		// 只暂停录像管线，直播/快照（写 DataDir 而非录像盘）不受影响。
		if w.diskTooLow() {
			if !w.diskPaused {
				w.diskPaused = true
				log.Printf("[%s] 磁盘可用空间低于 %dMB，暂停录像（清理后自动恢复）",
					w.dev.Name, w.mgr.cfg.MinFreeMB)
			}
			// 顺手触发一次清理：不等整点 tick，尽快腾出空间
			w.mgr.cleanupOldRecordings()
			select {
			case <-w.stop:
				return
			case <-time.After(time.Minute):
			}
			continue
		}
		if w.diskPaused {
			w.diskPaused = false
			log.Printf("[%s] 磁盘空间已恢复，继续录像", w.dev.Name)
		}
		if err := w.startProcs(); err != nil {
			log.Printf("[%s] start ffmpeg: %v", w.dev.Name, redactText(err.Error()))
			w.noteAuthFailure(redactText(err.Error()))
			if w.fail(&backoff) {
				return
			}
			continue
		}
		// 认证恢复：流起来了就清掉 401 标记与计数，后续失败回到普通退避
		w.lastAuthErrAt = time.Time{}
		w.authFails = 0
		_ = w.mgr.st.SetDeviceOnline(w.dev.ID, true)
		w.streamErrs = 0
		w.mgr.broadcastEvent("online", w.dev.ID, w.dev.Name, "", "设备在线", w.dev.Name+" 流已恢复", time.Now().Format(time.RFC3339))
		w.restarts = 0
		backoff = time.Second
		go w.drainFrames()

		// Check if processes are still alive after 2s
		time.Sleep(2 * time.Second)
		if w.recordProc != nil && !w.recordProc.Running() {
			out := redactText(w.recordProc.Log())
			w.noteAuthFailure(out) // 401 必须在这里就标记：进程随即被 killProcs 清掉，日志就没了
			w.markStreamError(out)
			w.logStderrThrottled(w.dev.Name+" record exited", out)
		}
		if w.liveProc != nil && !w.liveProc.Running() {
			out := redactText(w.liveProc.Log())
			w.noteAuthFailure(out)
			w.markStreamError(out)
			w.logStderrThrottled(w.dev.Name+" live exited", out)
		}
		if w.teeProc != nil && !w.teeProc.Running() {
			// 单连接模式：录像与 HLS 同在一个进程里，401 只可能出现在这里
			out := redactText(w.teeProc.Log())
			w.noteAuthFailure(out)
			w.markStreamError(out)
			w.logStderrThrottled(w.dev.Name+" tee exited", out)
		}
		if w.snapProc != nil && !w.snapProc.Running() {
			log.Printf("[%s] snap process exited early (non-fatal)", w.dev.Name)
		}

		go w.guardSnap()
		// 假死看门狗：进程活着但不再读数据时主动重启（只靠 Running() 检测不到）。
		// 用本轮独立通道，waitExit 返回后立即关闭，避免重启时看门狗逐次累积。
		iterStop := make(chan struct{})
		go w.stallWatchdog(iterStop)

		waitExit([]*ffmpeg.Proc{w.recordProc, w.liveProc, w.teeProc}, w.stop)
		close(iterStop)

		// 先取日志再 killProcs：killProcs 会把 teeProc 置 nil，
		// 之后再读 earlyLog() 就永远拿不到单连接进程的 401 输出。
		preKillLog := redactText(w.earlyLog())
		w.killProcs()
		w.noteAuthFailure(preKillLog)
		log.Printf("[%s] stream exited, restarting", w.dev.Name)
		if w.fail(&backoff) {
			return
		}
	}
}

func (w *Worker) shouldRecordNow() bool {
	// Per-device recording switch: if explicitly disabled, don't record.
	if !w.dev.RecordEnabled {
		return false
	}
	mode := w.dev.RecordMode
	if mode == "" {
		// Fall back to global strategy for devices configured before
		// per-device settings were introduced.
		if w.mgr.ShouldRecordFn == nil {
			return true
		}
		gMode, gStart, gEnd := w.mgr.ShouldRecordFn()
		mode, w.dev.ScheduleStart, w.dev.ScheduleEnd = gMode, gStart, gEnd
	}
	start, end := w.dev.ScheduleStart, w.dev.ScheduleEnd
	if start == "" {
		start = "08:00"
	}
	if end == "" {
		end = "20:00"
	}
	switch mode {
	case "schedule":
		return inScheduleRange(start, end)
	case "motion":
		return w.hasRecentActivity()
	case "continuous":
		return true
	default:
		return true
	}
}

func inScheduleRange(start, end string) bool {
	now := time.Now()
	sh, sm := parseHHMM(start)
	eh, em := parseHHMM(end)
	nowMin := now.Hour()*60 + now.Minute()
	startMin := sh*60 + sm
	endMin := eh*60 + em
	if startMin <= endMin {
		return nowMin >= startMin && nowMin < endMin
	}
	return nowMin >= startMin || nowMin < endMin
}

func parseHHMM(s string) (int, int) {
	if len(s) < 5 {
		return 0, 0
	}
	return atoi(s[:2]), atoi(s[3:5])
}

func (w *Worker) hasRecentActivity() bool {
	if w.mgr.ai == nil {
		return true
	}
	state := w.mgr.ai.State(w.dev.ID)
	if state == nil {
		return true
	}
	return time.Since(state.LastEventTime()) < 2*time.Minute
}

// authRetrySteps 认证失败（401）专用的长退避档位：1→5→15→30 分钟。
// 相机对连续错误登录会临时锁定账号（如错 7 次锁 30 分钟），快速重试等于
// 继续累加失败次数、把锁定越刷越长；期间既不重试也不计入永久离线计数。
var authRetrySteps = []time.Duration{
	1 * time.Minute, 5 * time.Minute, 15 * time.Minute, 30 * time.Minute,
}

// diskTooLow 检查录像盘剩余空间是否低于配置的最低水位。
// Statfs 取不到（非 POSIX / 权限不足）时视为「不低」，不误伤录像。
func (w *Worker) diskTooLow() bool {
	if w.mgr.cfg.MinFreeMB <= 0 {
		return false
	}
	free, ok := diskFreeBytes(w.mgr.cfg.RecordDir)
	if !ok {
		return false
	}
	return free < uint64(w.mgr.cfg.MinFreeMB)<<20
}

// earlyLog 返回所有子进程已捕获的 stderr 文本（供退出后补查 401 标记）。
func (w *Worker) earlyLog() string {
	var b strings.Builder
	for _, p := range []*ffmpeg.Proc{w.recordProc, w.liveProc, w.teeProc} {
		if p != nil {
			b.WriteString(p.Log())
		}
	}
	return b.String()
}

// containsFold 大小写不敏感的子串匹配。
func containsFold(s, sub string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(sub))
}

// noteAuthFailure 记录「刚刚发生过一次认证失败」的时刻。
// fail() 据此判断是否仍在认证失败窗口内（比较经过时间，而不是比较截止时刻——
// 若存的是 time.Now() 当作 deadline，fail() 里的 now 必然晚于它，判断恒为假）。
func (w *Worker) noteAuthFailure(reason string) {
	if containsFold(reason, "401") || containsFold(reason, "unauthorized") {
		w.lastAuthErrAt = time.Now()
	}
}

// authFailureWindow 是认证失败的认定窗口：最后一次 401 之后这么久内，
// 都按「认证失败」处理（走长退避），而不是按普通网络错误秒级重试。
const authFailureWindow = 30 * time.Second

// fail 判定一次拉流失败后的重试策略；返回 true 表示 worker 应当永久停止。
//
// 两套退避：
//   - 认证失败（401/unauthorized，含账号被相机临时锁定）：按 authRetrySteps
//     长退避等待，不累计 restarts——否则密码错几次就会被永久标记离线，
//     用户改对密码后仍看到「离线」；等待可被 Stop() 立即打断。
//   - 其它错误（网络断、相机重启、超时）：指数退避 1→2→…→30 秒，5 次后放弃。
//
// 标记存在 Worker 字段上（w.lastAuthErrAt）：noteAuthFailure 在两次 fail()
// 调用之间写入，用局部变量会永远读不到。
func (w *Worker) fail(backoff *time.Duration) bool {
	if !w.lastAuthErrAt.IsZero() && time.Since(w.lastAuthErrAt) < authFailureWindow {
		// 处于认证失败窗口：按 authRetrySteps 递增长退避
		w.authFails++
		n := w.authFails
		if n > len(authRetrySteps) {
			n = len(authRetrySteps)
		}
		d := authRetrySteps[n-1]
		log.Printf("[%s] 认证失败（401），%d 分钟后重试（第 %d 次；相机可能已临时锁定该账号，连续试错会延长锁定）",
			w.dev.Name, int(d.Minutes()), w.authFails)
		select {
		case <-w.stop:
			return false
		case <-time.After(d):
		}
		// 退避期间若一直连不上，窗口过期后自动回到普通退避
		return false
	}

	w.restarts++
	if w.restarts >= 5 {
		log.Printf("[%s] too many failures, marking offline", w.dev.Name)
		return true
	}
	time.Sleep(*backoff)
	if *backoff < 30*time.Second {
		*backoff *= 2
	}
	return false
}

// drainFrames copies current.jpg into the AI ring while procs are alive.
func (w *Worker) drainFrames() {
	if w.mgr.ai == nil {
		return
	}
	// Device-level toggle wins; when unset, fall back to the global setting.
	if w.dev.AIEnabled != nil {
		if !*w.dev.AIEnabled {
			return
		}
	} else if !w.mgr.cfg.AIEnabled {
		return
	}
	tick := time.NewTicker(time.Duration(w.mgr.cfg.SnapshotIntervalSec) * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-w.stop:
			return
		case <-tick.C:
			data, err := os.ReadFile(filepath.Join(w.snapDir, "current.jpg"))
			if err != nil {
				continue
			}
			w.mgr.ai.PushImage(w.dev.ID, data, time.Now().UnixMilli())
			w.mgr.ai.MaybeAnalyze(w.dev)
		}
	}
}

// stallWatchdog 周期性巡检本 worker 的 ffmpeg 进程是否假死；stop 关闭即退出。
// 每轮启动一个，随 waitExit 返回而结束，不会随重启累积。
func (w *Worker) stallWatchdog(stop chan struct{}) {
	tick := time.NewTicker(stallCheckInterval)
	defer tick.Stop()
	for {
		select {
		case <-stop:
			return
		case <-tick.C:
			w.watchProcs()
		}
	}
}

// stallWindow 是「假死」判定窗口：ffmpeg 进程存活但累计读取字节数在这么长
// 时间内完全不增长，说明它已不再从摄像头取流（卡在不可恢复的网络等待里），
// 此时只靠「进程是否存活」是检测不出来的，必须主动重启。
const stallWindow = 45 * time.Second

// stallCheckInterval 假死巡检间隔。
const stallCheckInterval = 15 * time.Second

// procReadBytes 读取进程累计读取字节数（/proc/<pid>/io 的 rchar）。
// rchar 统计的是 read() 层面的字节数，RTSP 收流会持续增长，是判断
// 「进程还在不在干活」最直接的指标。取不到（权限/非 Linux）返回 false。
func procReadBytes(pid int) (uint64, bool) {
	if pid <= 0 {
		return 0, false
	}
	b, err := os.ReadFile(fmt.Sprintf("/proc/%d/io", pid))
	if err != nil {
		return 0, false
	}
	// 格式：每行 "key: value"，取 rchar
	for _, line := range strings.Split(string(b), "\n") {
		if !strings.HasPrefix(line, "rchar:") {
			continue
		}
		if n, err := strconv.ParseUint(strings.TrimSpace(line[len("rchar:"):]), 10, 64); err == nil {
			return n, true
		}
	}
	return 0, false
}

// stallState 记录某个进程上一次的采样，用于比较是否还在推进。
type stallState struct {
	pid      int
	lastRead uint64
	lastAt   time.Time
}

// stalledSince 判断进程是否已达假死条件：读取计数相对基准没有增长，
// 且持续时间达到窗口。抽成纯函数便于单测（不依赖真实进程/时钟）。
//
// 返回 true 表示「应当重启该进程」。
func stalledSince(st *stallState, nowRead uint64, now time.Time, window time.Duration) bool {
	if st == nil || st.lastAt.IsZero() {
		return false
	}
	// 读数仍在增长 → 进程在干活，刷新基准
	if nowRead > st.lastRead {
		return false
	}
	return now.Sub(st.lastAt) >= window
}

// watchProcs 巡检本 worker 的所有 ffmpeg 进程，把假死的强杀掉。
// 杀掉后 supervise 循环会自然重启它（走既有的退避/长退避逻辑）。
//
// 单连接模式下 recordProc 与 teeProc 是同一个进程，用 map 去重避免重复判。
func (w *Worker) watchProcs() {
	if w.stall == nil {
		w.stall = map[string]*stallState{}
	}
	procs := map[string]*ffmpeg.Proc{
		"record": w.recordProc,
		"live":   w.liveProc,
		"tee":    w.teeProc,
	}
	// 用 pid 去重：单连接模式下 record/tee 指向同一进程
	seen := map[int]bool{}
	for name, p := range procs {
		if p == nil || !p.Running() {
			delete(w.stall, name)
			continue
		}
		pid := p.Pid()
		if pid <= 0 || seen[pid] {
			continue
		}
		seen[pid] = true
		rc, ok := procReadBytes(pid)
		if !ok {
			// 拿不到 io 统计（容器限制/权限）→ 放弃看门狗，避免误杀
			continue
		}
		st, exists := w.stall[name]
		now := time.Now()
		if !exists || st.pid != pid {
			w.stall[name] = &stallState{pid: pid, lastRead: rc, lastAt: now}
			continue
		}
		if rc > st.lastRead {
			// 仍在读数据：推进基准
			st.lastRead = rc
			st.lastAt = now
			continue
		}
		// 读数未增长：超过窗口即判假死
		if stalledSince(st, rc, now, stallWindow) {
			log.Printf("[%s] 看门狗：%s 进程 %d 已 %v 无读取增长，判定假死，强制重启",
				w.dev.Name, name, pid, now.Sub(st.lastAt).Round(time.Second))
			st.lastAt = now // 避免同一进程被连续重复判死
			st.lastRead = 0
			// Kill 会把进程标记为 killed，supervise 的 waitExit 随即返回并重启
			p.Kill()
		}
	}
}

func waitExit(procs []*ffmpeg.Proc, stop chan struct{}) {
	ch := make(chan struct{}, 1)
	for _, p := range procs {
		if p == nil {
			continue
		}
		go func(p *ffmpeg.Proc) {
			_ = p.Wait()
			select {
			case ch <- struct{}{}:
			default:
			}
		}(p)
	}
	select {
	case <-ch:
	case <-stop:
	}
}

// ---------- 按需抓拍（含并发去重） ----------

// snapInflight 是「同一设备同一时刻只抓一帧」的去重表。
//
// 为什么需要：GET /api/devices/:id/snapshot 会被客户端高频轮询（首页缩略图、
// 多个页面标签、多个客户端），而单次抓帧要起一个 ffmpeg、解码一路视频，
// 耗时数百毫秒到数秒。不做去重的话，并发请求会同时拉起多个 ffmpeg 抢同一路
// 视频，既浪费 CPU 又可能触发摄像头并发会话上限。
//
// 键为设备 ID，值为正在进行的抓帧结果通道；后来者直接复用同一结果。
var snapInflight sync.Map // map[string]*snapCall

type snapCall struct {
	done chan struct{}
	path string
	err  error
}

// GrabSnapshot 确保设备的 current.jpg 存在且足够新，返回其路径。
//
// maxAge 为该文件可接受的最大陈旧时间：文件比它新就直接复用（连 ffmpeg
// 都不用起）；否则触发一次抓帧。并发调用会被合并成一次实际抓帧。
func (m *Manager) GrabSnapshot(deviceID string, maxAge time.Duration) (string, error) {
	w := m.worker(deviceID)
	if w == nil {
		return "", fmt.Errorf("设备未运行，无法抓拍")
	}
	target := filepath.Join(w.snapDir, "current.jpg")

	// 命中新鲜缓存：直接返回，不抓帧
	if maxAge > 0 {
		if st, err := os.Stat(target); err == nil && time.Since(st.ModTime()) < maxAge {
			return target, nil
		}
	}

	// 并发去重：已有同设备的抓帧在进行中，等它的结果
	if v, ok := snapInflight.Load(deviceID); ok {
		call := v.(*snapCall)
		<-call.done
		return call.path, call.err
	}

	call := &snapCall{done: make(chan struct{})}
	// LoadOrStore 处理「刚被别人抢先插入」的竞态
	if actual, loaded := snapInflight.LoadOrStore(deviceID, call); loaded {
		prev := actual.(*snapCall)
		<-prev.done
		return prev.path, prev.err
	}
	defer func() {
		snapInflight.Delete(deviceID)
		close(call.done)
	}()

	call.path, call.err = w.captureFrame(target)
	return call.path, call.err
}

// captureFrame 起一个一次性 ffmpeg 抓一帧到 target（先写临时文件再原子改名，
// 避免读取方拿到写了一半的图片）。
func (w *Worker) captureFrame(target string) (string, error) {
	if w.recordProc == nil || !w.recordProc.Running() {
		return "", fmt.Errorf("录像管线未运行，无法抓拍")
	}
	if err := os.MkdirAll(w.snapDir, 0o755); err != nil {
		return "", err
	}
	tmp := target + ".tmp"
	args := append(w.snapInputArgs(),
		"-frames:v", "1", "-f", "image2", "-q:v", "4", "-y", tmp)
	p, err := ffmpeg.Start(w.mgr.cfg.Ffmpeg, args...)
	if err != nil || p == nil {
		return "", fmt.Errorf("启动抓帧进程失败: %v", err)
	}
	// 抓帧没硬超时会挂住（相机无响应时）：给 12 秒上限
	done := make(chan struct{})
	go func() {
		_ = p.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(12 * time.Second):
		p.Kill()
		return "", fmt.Errorf("抓帧超时")
	}
	if st, err := os.Stat(tmp); err != nil || st.Size() == 0 {
		_ = os.Remove(tmp)
		return "", fmt.Errorf("抓帧未产出有效图像")
	}
	if err := os.Rename(tmp, target); err != nil {
		_ = os.Remove(tmp)
		return "", err
	}
	return target, nil
}

// worker 按设备 ID 取运行中的 worker（不存在返回 nil）。
func (m *Manager) worker(deviceID string) *Worker {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.workers[deviceID]
}

// SnapshotPath 返回设备快照的落盘路径（不保证存在）。
func (m *Manager) SnapshotPath(deviceID string) string {
	w := m.worker(deviceID)
	if w == nil {
		return filepath.Join(m.cfg.SnapDir, deviceID, "current.jpg")
	}
	return filepath.Join(w.snapDir, "current.jpg")
}

// needSnapshots 判断本设备是否需要 AI 按秒取帧。
//
// 与「快照文件本身是否需要」是两件事：
//   - current.jpg 有两个消费者：AI 分析（drainFrames）与对外接口
//     GET /api/devices/:id/snapshot；
//   - 因此不能因为 AI 关闭就完全不产出快照，否则那个接口会恒 404。
//
// 这里的语义收窄为「是否需要常驻的高频抓帧」：AI 开启时才需要，
// AI 关闭时改由 GrabSnapshot 在有人请求时按需抓一帧（零常驻进程）。
func (w *Worker) needSnapshots() bool {
	if w.mgr.ai == nil {
		return false
	}
	// 设备级开关优先；未设置时跟随全局
	if w.dev.AIEnabled != nil {
		return *w.dev.AIEnabled
	}
	return w.mgr.cfg.AIEnabled
}

// snapInputArgs 返回抓帧的输入参数。
//
// 优先从 HLS 播放列表读（与直播同一路数据，零额外摄像头连接）；
// HLS 尚未就绪时回退到直连 RTSP。
func (w *Worker) snapInputArgs() []string {
	livePl := filepath.Join(w.liveDir, "index.m3u8")
	if _, err := os.Stat(livePl); err == nil {
		return []string{"-loglevel", "error", "-y", "-i", livePl}
	}
	return append([]string{"-loglevel", "error", "-y"}, w.inputArgs()...)
}

// guardSnap restarts the snapshot process if it exited, without disturbing
// the record/live pipeline. AI 关闭时不启动（见 needSnapshots）。
func (w *Worker) guardSnap() {
	if !w.needSnapshots() {
		// AI 被关掉后残留的快照进程要收掉，否则白占资源
		if w.snapProc != nil {
			w.snapProc.Kill()
			w.snapProc = nil
		}
		return
	}
	if w.snapProc != nil && w.snapProc.Running() {
		return
	}
	if w.recordProc == nil || !w.recordProc.Running() {
		return
	}
	snapArgs := append(w.snapInputArgs(),
		"-vf", "fps=1", "-update", "1", "-y", filepath.Join(w.snapDir, "current.jpg"))
	p, err := ffmpeg.Start(w.mgr.cfg.Ffmpeg, snapArgs...)
	if err != nil || p == nil {
		log.Printf("[%s] snap restart error (non-fatal): %v", w.dev.Name, err)
		return
	}
	if w.snapProc != nil {
		w.snapProc.Kill()
	}
	w.snapProc = p
	log.Printf("[%s] snap restarted", w.dev.Name)
}

// ---------- AI loop (redundant safety; per-device drain covers it) ----------

func (m *Manager) aiLoop() {
	tick := time.NewTicker(10 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-m.stopAll:
			return
		case <-tick.C:
			if !m.cfg.AIEnabled {
				continue
			}
			m.mu.Lock()
			workers := make([]*Worker, 0, len(m.workers))
			for _, w := range m.workers {
				workers = append(workers, w)
			}
			m.mu.Unlock()
			for _, w := range workers {
				data, err := os.ReadFile(filepath.Join(w.snapDir, "current.jpg"))
				if err != nil {
					continue
				}
				m.ai.PushImage(w.dev.ID, data, time.Now().UnixMilli())
				m.ai.MaybeAnalyze(w.dev)
			}
		}
	}
}

// ---------- segment indexing ----------

var segNameRe = regexp.MustCompile(`^(\d{6})\.mp4$`)
var dayDirRe = regexp.MustCompile(`^\d{8}$`)

func (m *Manager) indexerLoop() {
	tick := time.NewTicker(15 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-m.stopAll:
			return
		case <-tick.C:
			m.indexSegments()
		}
	}
}

// retryLoop periodically restarts workers for devices that are not running
// (e.g. after repeated failures or added while ffmpeg was missing).
func (m *Manager) retryLoop() {
	tick := time.NewTicker(time.Minute)
	defer tick.Stop()
	for {
		select {
		case <-m.stopAll:
			return
		case <-tick.C:
			devs, err := m.st.ListDevices()
			if err != nil {
				continue
			}
			for _, d := range devs {
				if !m.IsRunning(d.ID) {
					m.StartWorker(&d)
				}
			}
		}
	}
}

func (m *Manager) indexSegments() {
	days, err := os.ReadDir(m.cfg.RecordDir)
	if err != nil {
		return
	}
	for _, d := range days {
		if !d.IsDir() {
			continue
		}
		m.indexDeviceDay(d.Name(), filepath.Join(m.cfg.RecordDir, d.Name()))
	}
}

func (m *Manager) indexDeviceDay(deviceID, dayDir string) {
	subs, err := os.ReadDir(dayDir)
	if err != nil {
		return
	}
	for _, sub := range subs {
		if !sub.IsDir() || !dayDirRe.MatchString(sub.Name()) {
			continue
		}
		dateStr := sub.Name()
		yy := atoi(dateStr[0:4])
		mm := atoi(dateStr[4:6])
		dd := atoi(dateStr[6:8])

		type segInfo struct {
			start time.Time
			path  string
			name  string
		}
		var segs []segInfo

		files, err := os.ReadDir(filepath.Join(dayDir, sub.Name()))
		if err != nil {
			continue
		}
		for _, f := range files {
			if f.IsDir() {
				continue
			}
			match := segNameRe.FindStringSubmatch(f.Name())
			if match == nil {
				continue
			}
			hh := atoi(match[1][0:2])
			mi := atoi(match[1][2:4])
			ss := atoi(match[1][4:6])
			start := time.Date(yy, time.Month(mm), dd, hh, mi, ss, 0, time.Local)
			segs = append(segs, segInfo{
				start: start,
				path:  filepath.Join(dayDir, sub.Name(), f.Name()),
				name:  f.Name(),
			})
		}

		sort.Slice(segs, func(i, j int) bool { return segs[i].start.Before(segs[j].start) })

		for i, sg := range segs {
			end := sg.start.Add(5 * time.Minute)
			if i+1 < len(segs) {
				nextStart := segs[i+1].start
				if nextStart.After(sg.start) && nextStart.Before(end.Add(2*time.Minute)) {
					end = nextStart
				}
			}
			seg := models.RecordingSegment{
				ID:       deviceID + "_" + sg.start.Format("20060102150405"),
				DeviceID: deviceID,
				Start:    sg.start,
				End:      end,
				Path:     sg.path,
			}
			if err := m.st.UpsertSegment(seg); err != nil {
				log.Printf("upsert segment: %v", err)
			}
		}
	}
}

func atoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}

// ---------- retention cleanup ----------

func (m *Manager) cleanupLoop() {
	// 启动后先做一次：保留策略、损坏尾段、空目录都依赖它。
	// 只放在整点 tick 里会导致「服务重启后要等 1 小时才清理」——
	// 实测重装/升级后曾积压上百个半截文件与空目录。
	// 延后 2 分钟执行，避开启动瞬间的 ffmpeg 拉起高峰与大目录遍历叠加。
	select {
	case <-m.stopAll:
		return
	case <-time.After(2 * time.Minute):
		m.cleanupOldRecordings()
	}

	tick := time.NewTicker(1 * time.Hour)
	defer tick.Stop()
	for {
		select {
		case <-m.stopAll:
			return
		case <-tick.C:
			m.cleanupOldRecordings()
		}
	}
}

func (m *Manager) cleanupOldRecordings() {
	globalDays := m.cfg.RetentionDays
	if globalDays <= 0 {
		globalDays = 30
	}
	now := time.Now()

	// 单摄限额表：设备配置 >0 时覆盖全局。
	type limit struct {
		days   int
		sizeGB int
	}
	limits := map[string]limit{}
	if devs, err := m.st.ListDevices(); err == nil {
		for _, d := range devs {
			limits[d.ID] = limit{days: d.RetentionDays, sizeGB: d.RetentionSizeGB}
		}
	}

	// dayRec 是按日聚合的录像候选（清理/容量淘汰/低水位共用）。
	type dayRec = recDay
	// dirSize 统计目录字节数（失败的文件忽略，只求近似值）。
	dirSize := func(p string) int64 {
		var total int64
		_ = filepath.WalkDir(p, func(_ string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			if info, err := d.Info(); err == nil {
				total += info.Size()
			}
			return nil
		})
		return total
	}
	// trimToSize 从最旧开始删除，直到总量 <= keepBytes；返回保留项。
	trimToSize := func(items []dayRec, keepBytes int64) []dayRec {
		var total int64
		for i := range items {
			if !items[i].sizeOK {
				items[i].size = dirSize(items[i].path)
				items[i].sizeOK = true
			}
			total += items[i].size
		}
		if total <= keepBytes {
			return items
		}
		kept := items[:0]
		for _, it := range items {
			if total > keepBytes {
				if err := os.RemoveAll(it.path); err != nil {
					log.Printf("cleanup remove %s: %v", it.path, err)
					kept = append(kept, it)
					continue
				}
				log.Printf("cleanup removed recordings over size limit: %s", it.path)
				total -= it.size
				continue
			}
			kept = append(kept, it)
		}
		return kept
	}

	deviceDirs, err := os.ReadDir(m.cfg.RecordDir)
	if err != nil {
		return
	}

	// 1) 逐设备：先按保留天数删，再按单摄容量限额删。
	var flat []dayRec
	for _, dd := range deviceDirs {
		if !dd.IsDir() {
			continue
		}
		deviceID := dd.Name()
		lim := limits[deviceID]
		days := globalDays
		if lim.days > 0 {
			days = lim.days
		}
		cutoff := now.AddDate(0, 0, -days)

		dayDirs, err := os.ReadDir(filepath.Join(m.cfg.RecordDir, deviceID))
		if err != nil {
			continue
		}
		var items []dayRec
		for _, dayDir := range dayDirs {
			if !dayDir.IsDir() || !dayDirRe.MatchString(dayDir.Name()) {
				continue
			}
			dateStr := dayDir.Name()
			dayTime := time.Date(atoi(dateStr[0:4]), time.Month(atoi(dateStr[4:6])),
				atoi(dateStr[6:8]), 0, 0, 0, 0, time.Local)
			info := dayRec{
				path: filepath.Join(m.cfg.RecordDir, deviceID, dayDir.Name()),
				day:  dayTime,
			}
			if dayTime.Before(cutoff) {
				if err := os.RemoveAll(info.path); err != nil {
					log.Printf("cleanup remove %s: %v", info.path, err)
					items = append(items, info)
				} else {
					log.Printf("cleanup removed old recordings: %s", info.path)
				}
				continue
			}
			items = append(items, info)
		}
		// ReadDir 按文件名排序 = 日期升序，最旧在前，正好用于容量淘汰。
		if lim.sizeGB > 0 {
			items = trimToSize(items, int64(lim.sizeGB)<<30)
		}
		flat = append(flat, items...)
	}

	// 2) 全局总容量限额：跨设备按日期从旧到新淘汰。
	if m.cfg.RetentionSizeGB > 0 && len(flat) > 0 {
		sort.Slice(flat, func(i, j int) bool { return flat[i].day.Before(flat[j].day) })
		flat = trimToSize(flat, int64(m.cfg.RetentionSizeGB)<<30)
	}

	// 3) 事件仍按全局保留天数清理。
	if _, err := m.st.DeleteEventsBefore(now.AddDate(0, 0, -globalDays)); err != nil {
		log.Printf("cleanup delete events error: %v", err)
	}

	// 4) 最低剩余空间兜底：保留策略都没设限时（0=不限），
	// 磁盘快满也要按「从旧到新」删到恢复水位，避免写满整盘。
	m.enforceMinFree(flat)

	// 5) 半截文件清理：跳闸/断电/进程被杀会留下没有 moov 的 mp4，
	// 浏览器打不开、索引也认不出，白占空间。
	m.sweepBrokenSegments()

	// 6) 空目录回收：删除录像后 deviceID/YYYYMMDD 会留下空壳，
	// 长期运行会堆积大量空目录。放在最后做，此时可删的目录已全部删完。
	m.pruneEmptyDirs(m.cfg.RecordDir, 0)
}

// brokenSweepMinAge 是「半截文件」的最小存活时间：太新的文件可能正在写，
// 绝不能动（ffmpeg 分段 muxer 只在收尾时才把 moov 写进去）。
const brokenSweepMinAge = 3 * time.Minute

// brokenSweepMaxAge 只清理近期窗口内的残file；更老的损坏文件通常早已被
// 保留策略删除，扫描它们只会徒增 IO。超过该年龄的由保留策略负责。
const brokenSweepMaxAge = 24 * time.Hour

// sweepBrokenSegments 扫描录像目录，删除「已停止写入但没有 moov」的 mp4。
//
// 判定依据：
//   - mtime 距今在 [3 分钟, 24 小时) 之间 → 已停止写入，不是正在录的段；
//   - 文件里找不到 moov box → 索引缺失，无法播放。
//
// 只读文件头 64KB 判断（moov 一定在文件头部附近或被 faststart 前置）；
// 用 mtime 作为缓存键，避免每小时重复读同一批文件。
func (m *Manager) sweepBrokenSegments() {
	root := m.cfg.RecordDir
	if root == "" {
		return
	}
	now := time.Now()
	var scanned, removed int
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		// 只处理录像分段（*.mp4），跳过 playback 里的 HLS 产物
		if !strings.HasSuffix(strings.ToLower(d.Name()), ".mp4") {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		age := now.Sub(info.ModTime())
		if age < brokenSweepMinAge || age > brokenSweepMaxAge {
			return nil
		}
		if info.Size() == 0 {
			// 0 字节文件必然是坏段
			if os.Remove(p) == nil {
				removed++
				log.Printf("sweep: 删除 0 字节录像 %s", p)
			}
			return nil
		}
		scanned++
		if hasMoovBox(p, info.ModTime()) {
			return nil
		}
		if err := os.Remove(p); err != nil {
			log.Printf("sweep remove %s: %v", p, err)
			return nil
		}
		removed++
		return nil
	})
	if err != nil {
		log.Printf("sweep walk error: %v", err)
	}
	if removed > 0 {
		log.Printf("sweep: 已清理损坏录像分段 %d 个（扫描 %d 个）", removed, scanned)
	}
}

// moovCache 缓存「文件是否含 moov」，键为路径，值为 mtime 与结论。
// 录像文件一旦收尾就不会再变，用 mtime 命中即可跳过重复读取。
var moovCache sync.Map // map[string]moovEntry

type moovEntry struct {
	mtime   time.Time
	hasMoov bool
}

// hasMoovBox 读取文件头部判断是否包含 moov box。
// mp4 的 moov 可能写在头部（faststart）或尾部；分段录像收尾时 ffmpeg 会
// 把 moov 写在文件末尾，因此头尾各读一段更可靠。
func hasMoovBox(path string, mtime time.Time) bool {
	if v, ok := moovCache.Load(path); ok {
		if e, ok := v.(moovEntry); ok && e.mtime.Equal(mtime) {
			return e.hasMoov
		}
	}
	has := scanForMoov(path)
	moovCache.Store(path, moovEntry{mtime: mtime, hasMoov: has})
	return has
}

// scanForMoov 在文件头 64KB 与尾 64KB 中查找 "moov" 标记。
func scanForMoov(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		// 读不到就当作「完好」，避免误删
		return true
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return true
	}
	size := st.Size()
	const chunk = 64 * 1024
	target := []byte("moov")

	check := func(off int64) bool {
		buf := make([]byte, chunk)
		n, err := f.ReadAt(buf, off)
		if n <= 0 {
			_ = err
			return false
		}
		return bytes.Contains(buf[:n], target)
	}
	if size <= chunk {
		return check(0)
	}
	if check(0) {
		return true
	}
	return check(size - chunk)
}

// pruneEmptyDirs 自底向上删除 root 下的空目录（保留 root 自身）。
// depth 用于限制递归深度，避免异常目录结构导致深层遍历。
func (m *Manager) pruneEmptyDirs(dir string, depth int) {
	if depth > 6 {
		return
	}
	ents, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	removed := 0
	for _, e := range ents {
		if !e.IsDir() {
			continue
		}
		p := filepath.Join(dir, e.Name())
		// 先递归处理子目录
		m.pruneEmptyDirs(p, depth+1)
		// 子目录清空后若自身为空则删除（Remove 对非空目录会失败，天然安全）
		if sub, err := os.ReadDir(p); err == nil && len(sub) == 0 {
			if os.Remove(p) == nil {
				removed++
			}
		}
	}
	if removed > 0 {
		log.Printf("prune: 已清理空目录 %d 个（%s）", removed, dir)
	}
}

// recDay 是一段按日聚合的录像候选（清理/容量淘汰/低水位共用）。
type recDay struct {
	path   string
	day    time.Time
	size   int64
	sizeOK bool
}

// enforceMinFree 当录像盘剩余空间低于 MinFreeMB 时，按日期从旧到新逐个
// 删除录像日目录，直到恢复水位或无候选可删。正常情况下水位充足，
// 此函数是一次 Statfs + 一次比较的空操作。
func (m *Manager) enforceMinFree(candidates []recDay) {
	if m.cfg.MinFreeMB <= 0 || len(candidates) == 0 {
		return
	}
	free, ok := diskFreeBytes(m.cfg.RecordDir)
	if !ok {
		return
	}
	need := uint64(m.cfg.MinFreeMB) << 20
	if free >= need {
		return
	}
	log.Printf("cleanup: 录像盘剩余 %dMB 低于水位 %dMB，开始按日期从旧到新清理", free>>20, m.cfg.MinFreeMB)
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].day.Before(candidates[j].day) })
	for _, it := range candidates {
		if free >= need {
			break
		}
		info, err := os.Stat(it.path)
		if err != nil {
			continue
		}
		if err := os.RemoveAll(it.path); err != nil {
			log.Printf("cleanup minfree remove %s: %v", it.path, err)
			continue
		}
		free += uint64(info.Size())
		log.Printf("cleanup minfree removed: %s", it.path)
	}
}

func (m *Manager) broadcastEvent(eventType, deviceID, deviceName, eventID, label, desc, t string) {
	if m.onEvent != nil {
		m.onEvent(eventType, deviceID, deviceName, eventID, label, desc, t)
	}
}
