package recorder

import (
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
	teeSpec := fmt.Sprintf(
		"[f=segment:segment_time=300:reset_timestamps=1:strftime=1]%s|[f=hls:hls_time=2:hls_list_size=4:hls_flags=delete_segments]%s",
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

	// 快照从 HLS 播放列表读取：不新增 RTSP 连接
	snapArgs := []string{
		"-loglevel", "error", "-y",
		"-i", livePl,
		"-vf", "fps=1", "-update", "1", filepath.Join(w.snapDir, "current.jpg"),
	}
	w.snapProc, _ = ffmpeg.Start(w.mgr.cfg.Ffmpeg, snapArgs...)

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
	recArgs = append(recArgs,
		"-f", "segment", "-segment_time", "300", "-reset_timestamps", "1", "-strftime", "1",
		filepath.Join(w.recordDir, "%Y%m%d", "%H%M%S.mp4"))

	liveArgs := append(append([]string{}, in...), "-an")
	if transcode {
		liveArgs = append(liveArgs, transcodeArgs(w.mgr.cfg.Ffmpeg)...)
	} else {
		liveArgs = append(liveArgs, "-c:v", "copy")
	}
	liveArgs = append(liveArgs,
		"-f", "hls", "-hls_time", "2", "-hls_list_size", "4", "-hls_flags", "delete_segments",
		filepath.Join(w.liveDir, "index.m3u8"))

	snapArgs := append(append([]string{}, in...),
		"-vf", "fps=1", "-update", "1", "-y", filepath.Join(w.snapDir, "current.jpg"))

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
	log.Printf("[%s] starting ffmpeg: snap=%v", w.dev.Name, sanitizeArgs(snapArgs))
	w.snapProc, _ = ffmpeg.Start(w.mgr.cfg.Ffmpeg, snapArgs...)
	if w.snapProc == nil {
		log.Printf("[%s] snap start error (non-fatal)", w.dev.Name)
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
		if err := w.startProcs(); err != nil {
			log.Printf("[%s] start ffmpeg: %v", w.dev.Name, redactText(err.Error()))
			if w.fail(&backoff) {
				return
			}
			continue
		}
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
			log.Printf("[%s] record process exited early, stderr: %s", w.dev.Name, out)
			w.markStreamError(out)
		}
		if w.liveProc != nil && !w.liveProc.Running() {
			out := redactText(w.liveProc.Log())
			log.Printf("[%s] live process exited early, stderr: %s", w.dev.Name, out)
			w.markStreamError(out)
		}
		if w.snapProc != nil && !w.snapProc.Running() {
			log.Printf("[%s] snap process exited early (non-fatal)", w.dev.Name)
		}

		go w.guardSnap()

		waitExit([]*ffmpeg.Proc{w.recordProc, w.liveProc}, w.stop)

		w.killProcs()
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

// fail increments restart counter; returns true when worker should stop permanently.
func (w *Worker) fail(backoff *time.Duration) bool {
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

// guardSnap restarts the snapshot process if it exited, without disturbing
// the record/live pipeline.
func (w *Worker) guardSnap() {
	if w.snapProc != nil && w.snapProc.Running() {
		return
	}
	if w.recordProc == nil || !w.recordProc.Running() {
		return
	}
	in := w.inputArgs()
	snapArgs := append(append([]string{}, in...),
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
	days := m.cfg.RetentionDays
	if days <= 0 {
		days = 30
	}
	cutoff := time.Now().AddDate(0, 0, -days)

	deviceDirs, err := os.ReadDir(m.cfg.RecordDir)
	if err != nil {
		return
	}
	for _, dd := range deviceDirs {
		if !dd.IsDir() {
			continue
		}
		deviceID := dd.Name()
		dayDirs, err := os.ReadDir(filepath.Join(m.cfg.RecordDir, deviceID))
		if err != nil {
			continue
		}
		for _, dayDir := range dayDirs {
			if !dayDir.IsDir() || !dayDirRe.MatchString(dayDir.Name()) {
				continue
			}
			dateStr := dayDir.Name()
			yy := atoi(dateStr[0:4])
			mm := atoi(dateStr[4:6])
			dd2 := atoi(dateStr[6:8])
			dayTime := time.Date(yy, time.Month(mm), dd2, 0, 0, 0, 0, time.Local)
			if dayTime.Before(cutoff) {
				dirPath := filepath.Join(m.cfg.RecordDir, deviceID, dayDir.Name())
				if err := os.RemoveAll(dirPath); err != nil {
					log.Printf("cleanup remove %s: %v", dirPath, err)
				} else {
					log.Printf("cleanup removed old recordings: %s", dirPath)
				}
			}
		}
	}

	if _, err := m.st.DeleteEventsBefore(cutoff); err != nil {
		log.Printf("cleanup delete events error: %v", err)
	}
}

func (m *Manager) broadcastEvent(eventType, deviceID, deviceName, eventID, label, desc, t string) {
	if m.onEvent != nil {
		m.onEvent(eventType, deviceID, deviceName, eventID, label, desc, t)
	}
}
