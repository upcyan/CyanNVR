package hls

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"cyannvr/server/config"
	"cyannvr/server/pkg/ffmpeg"
	"cyannvr/server/store"
)

// 回放会话目录的回收策略。
const (
	// playbackTTL 是回放会话目录在「停止更新」后的保留时长。
	// ffmpeg 转码完整个录像片段后目录不再变化，此后保留一小段时间
	// 供用户暂停或重连，超时即回收。
	// 此前该值为 2 小时，且只在有人发起回放时才顺带清理，
	// 实测导致 10 个会话目录堆积 332MB。
	playbackTTL = 20 * time.Minute

	// playbackMaxBytes 是 playback 目录的总量上限；超出时按最旧优先回收，
	// 避免长时间无人回放时旧会话无限累积吃满磁盘。
	playbackMaxBytes = 2 << 30 // 2 GiB

	// playbackSweepInterval 是后台回收的扫描间隔。
	playbackSweepInterval = 5 * time.Minute
)

type Hls struct {
	cfg *config.Config
	st  *store.Store

	// 会话名 → 转码进程。用于回收时一并结束进程：
	// 回放转码是「一次性顺序转码整个请求窗口」，用户看完/离开后进程
	// 仍会继续跑（实测占 80% CPU），必须能被主动收掉，否则每次回放
	// 都会留下一个长期占用编码器的 ffmpeg。
	mu   sync.Mutex
	proc map[string]*ffmpeg.Proc
}

func New(cfg *config.Config, st *store.Store) *Hls {
	h := &Hls{cfg: cfg, st: st, proc: map[string]*ffmpeg.Proc{}}
	go h.janitor()
	return h
}

// janitor 周期性回收回放会话目录。
// 不能只依赖 CreatePlayback 里的清理：没人回放时旧目录会一直堆积。
func (h *Hls) janitor() {
	h.sweep()
	t := time.NewTicker(playbackSweepInterval)
	defer t.Stop()
	for range t.C {
		h.sweep()
	}
}

// Session describes a generated playback HLS session directory.
type Session struct {
	Dir  string
	Name string
}

// CreatePlayback builds a continuous HLS from recorded segments covering [start, end].
func (h *Hls) CreatePlayback(deviceID string, start, end time.Time, transcode bool) (*Session, error) {
	h.sweep()
	// 查询区间必须覆盖**整个请求窗口**，而不是只取 start 那一天。
	//
	// 两个坑：
	//  1. 前端窗口是 [起点-2min, 起点+10min]，用户在 23:50 之后点播时
	//     end 会落到次日；
	//  2. 前端用 toISOString() 发 UTC（"…T21:54:04.000Z"），解析后
	//     Location() 是 UTC。若直接按这个时区取日界，会与录像文件所在
	//     的本地日期错开 8 小时，导致「明明有录像却说没有」。
	//
	// 因此这里先转本地时区，再按本地日期各向前后扩一天，确保不会漏段。
	local := time.Local
	ls, le := start.In(local), end.In(local)
	segFrom := time.Date(ls.Year(), ls.Month(), ls.Day(), 0, 0, 0, 0, local).AddDate(0, 0, -1)
	segTo := time.Date(le.Year(), le.Month(), le.Day(), 0, 0, 0, 0, local).AddDate(0, 0, 1)
	if !segTo.After(segFrom) {
		segTo = segFrom.AddDate(0, 0, 1)
	}
	segs, err := h.st.SegmentsForDay(deviceID, segFrom, segTo)
	if err != nil {
		return nil, err
	}
	var files []string
	var firstStart time.Time
	for _, s := range segs {
		if s.End.Before(start) || s.Start.After(end) {
			continue
		}
		if _, err := os.Stat(s.Path); err != nil {
			continue
		}
		if len(files) == 0 {
			firstStart = s.Start
		}
		files = append(files, s.Path)
	}
	if len(files) == 0 {
		// 面向用户的提示：该时间点附近确实没有录像文件
		// （常见于当天服务重启过、或定时录像的空档时段）
		return nil, fmt.Errorf("该时间点附近没有录像（可能处于录像空档）")
	}
	name := uuid.NewString()
	dir := filepath.Join(h.cfg.LiveDir, "playback", name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	listPath := filepath.Join(dir, "input.txt")
	var b strings.Builder
	for _, f := range files {
		abs, err := filepath.Abs(f)
		if err != nil {
			abs = f
		}
		b.WriteString("file '")
		b.WriteString(filepath.ToSlash(abs))
		b.WriteString("'\n")
	}
	if err := os.WriteFile(listPath, []byte(b.String()), 0o644); err != nil {
		return nil, err
	}
	rel := start.Sub(firstStart).Seconds()
	if rel < 0 {
		rel = 0
	}
	dur := end.Sub(start).Seconds()
	if dur <= 0 {
		dur = 60
	}
	// 回放刻意不做硬件解码。
	//
	// 实测（HEVC 2880x1620 源，请求区间中段，输出首个 3 秒分片）：
	//   带 -hwaccel cuda ：约 40 秒
	//   纯软解 + h264_nvenc：约 19 秒
	// 原因是 concat 解复用器读的是本地 mp4，硬解会把每帧搬进显存再取回，
	// 而回放是「一次性顺序读」而非直播那种长连接流，显存往返纯属额外开销。
	// 直播路径（recorder）仍用硬解，那里是持续拉流、收益为正。
	//
	// 注意：这里只跳过解码侧的硬解参数，编码侧的 hw.EncodeArgs() 照旧使用。
	hw := ffmpeg.ProbeHW(h.cfg.Ffmpeg)
	args := []string{"-loglevel", "error", "-y"}
	// 缩小探测范围：concat 输入默认会读很大一段来识别流信息，
	// 对多片段拼接会明显拖慢起播。
	args = append(args,
		"-probesize", "5M", "-analyzeduration", "2M",
		"-f", "concat", "-safe", "0", "-i", listPath,
		"-ss", fmt.Sprintf("%.3f", rel),
		"-t", fmt.Sprintf("%.3f", dur),
		"-an",
	)
	if transcode {
		args = append(args, hw.EncodeArgs()...)
	} else {
		args = append(args, "-c:v", "copy")
	}
	// 首片时长决定「用户要等多久才看到画面」：ffmpeg 必须先写完一个分片
	// 才会生成 index.m3u8。实测瓶颈不在切片时长（3 秒片与 1 秒片的
	// 首片延迟几乎相同：转码路径都要 ~7 秒，主要是 CUDA 硬解初始化与
	// concat 建连的固定开销），因此不加 -hls_init_time，避免多切分片
	// 增加请求数却换不来起播速度。
	args = append(args, "-f", "hls",
		"-hls_time", "3",
		"-hls_list_size", "0",
		"-hls_flags", "delete_segments+temp_file",
		filepath.Join(dir, "index.m3u8"))

	proc, err := ffmpeg.Start(h.cfg.Ffmpeg, args...)
	if err != nil {
		return nil, err
	}
	h.registerProc(name, proc)
	// 兜底：进程自然结束（转码完请求区间）后从表中摘除，避免表无限增长
	go func() {
		_ = proc.Wait()
		h.mu.Lock()
		if h.proc[name] == proc {
			delete(h.proc, name)
		}
		h.mu.Unlock()
	}()
	// 硬上限：即使前端一直不关页面，也不让单个会话转码超过 2 小时
	go func() {
		<-time.After(2 * time.Hour)
		proc.Kill()
	}()

	// 等待播放列表就绪后再返回。
	//
	// ffmpeg 必须先编码完首个分片才会写出 index.m3u8。实测（HEVC 2880x1620、
	// 请求区间位于片段中段）：
	//   直接 copy（H.264 源）：        约 0.4 秒
	//   转码（H.265 源，本函数当前实现）：约 18 秒
	// 转码的耗时主要在 seek 到目标位置后的首段解码 + 首片编码，无法通过
	// 调整切片时长规避（1 秒片与 3 秒片的首片延迟基本相同）。
	//
	// 关键：**超时必须返回错误**，不能返回一个指向尚未生成的播放列表的 URL。
	// 前端拿到 URL 会立刻加载，此时 404 会被当成「回放坏了」——
	// 这正是此前 10 秒超时后回放页打不开的原因。
	playlist := filepath.Join(dir, "index.m3u8")
	deadline := time.Now().Add(playbackReadyTimeout)
	ready := false
	for time.Now().Before(deadline) {
		if fi, statErr := os.Stat(playlist); statErr == nil && fi.Size() > 0 {
			ready = true
			break
		}
		if proc != nil && !proc.Running() {
			break // ffmpeg 已退出（如源文件损坏），无需再等
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !ready {
		// 清掉这次没成功的会话，避免堆积空目录占空间
		proc.Kill()
		_ = os.RemoveAll(dir)
		if proc != nil && !proc.Running() {
			if log := strings.TrimSpace(proc.Log()); log != "" {
				return nil, fmt.Errorf("回放转码失败：%s", firstLineOf(log))
			}
		}
		return nil, fmt.Errorf("回放准备超时（%v）：该时段可能需要转码，请稍后重试或缩短时间范围", playbackReadyTimeout)
	}
	return &Session{Dir: dir, Name: name}, nil
}

// playbackReadyTimeout 是等待首个 HLS 分片（index.m3u8 落盘）的上限。
//
// 取 45 秒：实测 H.265 源转码首个 3 秒分片约需 18 秒（2880x1620），
// 留出充足余量以覆盖多路录像同时在转码、CPU/GPU 繁忙的情形。
// 超时远小于此会让「稍慢但正常」的回放被判失败。
const playbackReadyTimeout = 45 * time.Second

// firstLineOf 取多行文本的首个非空行（ffmpeg 报错常带一堆上下文）。
func firstLineOf(s string) string {
	for _, line := range strings.Split(s, "\n") {
		if t := strings.TrimSpace(line); t != "" {
			if len(t) > 200 {
				t = t[:200] + "…"
			}
			return t
		}
	}
	return ""
}

// sweep 回收回放会话目录：先删除过期会话，再按总量上限做最旧优先回收。
func (h *Hls) sweep() {
	base := filepath.Join(h.cfg.LiveDir, "playback")
	entries, err := os.ReadDir(base)
	if err != nil {
		return
	}
	type session struct {
		path string
		mod  time.Time
		size int64
	}
	var (
		kept  []session
		total int64
		now   = time.Now()
	)
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		path := filepath.Join(base, e.Name())
		if now.Sub(info.ModTime()) > playbackTTL {
			h.stopSession(e.Name())
			_ = os.RemoveAll(path)
			continue
		}
		size := dirSize(path)
		kept = append(kept, session{path: path, mod: info.ModTime(), size: size})
		total += size
	}
	if total <= playbackMaxBytes {
		return
	}
	// 超限：从最旧的开始回收，直到降到上限以内。
	sort.Slice(kept, func(i, j int) bool { return kept[i].mod.Before(kept[j].mod) })
	for _, s := range kept {
		if total <= playbackMaxBytes {
			break
		}
		h.stopSession(filepath.Base(s.path))
		_ = os.RemoveAll(s.path)
		total -= s.size
	}
}

// registerProc 记录会话对应的转码进程。
func (h *Hls) registerProc(name string, p *ffmpeg.Proc) {
	h.mu.Lock()
	h.proc[name] = p
	h.mu.Unlock()
}

// stopSession 结束会话的转码进程（若仍在运行）。
// 返回 true 表示确实杀掉了进程；目录删除由调用方负责。
func (h *Hls) stopSession(name string) bool {
	h.mu.Lock()
	p := h.proc[name]
	delete(h.proc, name)
	h.mu.Unlock()
	if p == nil {
		return false
	}
	if p.Running() {
		p.Kill()
		return true
	}
	return false
}

// StopPlayback 由 HTTP 层调用：用户离开回放页时立即结束会话，
// 不必等 TTL 到期（否则转码会白跑十几分钟、持续占用编码器）。
func (h *Hls) StopPlayback(name string) bool {
	return h.stopSession(name)
}

// ProcCount 返回仍在跟踪的会话进程数（供状态接口/诊断使用）。
func (h *Hls) ProcCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	n := 0
	for _, p := range h.proc {
		if p != nil && p.Running() {
			n++
		}
	}
	return n
}

// dirSize 统计目录占用的字节数；读取失败的条目按 0 计，不影响回收主流程。
func dirSize(dir string) int64 {
	var n int64
	_ = filepath.WalkDir(dir, func(_ string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if fi, err := d.Info(); err == nil {
			n += fi.Size()
		}
		return nil
	})
	return n
}

// SegmentExists reports whether a file exists within a live/session dir (path traversal safe).
func (h *Hls) ResolveFile(rootDir, file string) (string, bool) {
	clean := filepath.Clean("/" + filepath.ToSlash(file))
	clean = strings.TrimPrefix(clean, "/")
	p := filepath.Join(rootDir, filepath.FromSlash(clean))
	if !strings.HasPrefix(p, rootDir) {
		return "", false
	}
	if _, err := os.Stat(p); err != nil {
		return "", false
	}
	return p, true
}
