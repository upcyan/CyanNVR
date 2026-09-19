package hls

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"simplenvr/server/config"
	"simplenvr/server/pkg/ffmpeg"
	"simplenvr/server/store"
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
}

func New(cfg *config.Config, st *store.Store) *Hls {
	h := &Hls{cfg: cfg, st: st}
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
	segs, err := h.st.SegmentsForDay(deviceID,
		time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, start.Location()),
		time.Date(start.Year(), start.Month(), start.Day()+1, 0, 0, 0, 0, start.Location()))
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
		return nil, fmt.Errorf("no recordings in range")
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
	// 硬件解码参数必须置于 -i 之前
	hw := ffmpeg.ProbeHW(h.cfg.Ffmpeg)
	args := []string{"-loglevel", "error", "-y"}
	args = append(args, hw.DecodeArgs()...)
	args = append(args,
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
	args = append(args, "-f", "hls", "-hls_time", "3", "-hls_list_size", "0",
		"-hls_flags", "delete_segments+temp_file", filepath.Join(dir, "index.m3u8"))

	proc, err := ffmpeg.Start(h.cfg.Ffmpeg, args...)
	if err != nil {
		return nil, err
	}
	go func() {
		<-time.After(12 * time.Hour)
		proc.Kill()
	}()

	// 等待播放列表就绪后再返回。
	// ffmpeg 必须先编码完首个分片才会写出 index.m3u8（转码场景通常 1-3 秒），
	// 若直接返回 URL，前端立即加载会得到 404 —— 这正是回放页打不开的原因。
	playlist := filepath.Join(dir, "index.m3u8")
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if fi, statErr := os.Stat(playlist); statErr == nil && fi.Size() > 0 {
			break
		}
		if proc != nil && !proc.Running() {
			break // ffmpeg 已退出（如源文件损坏），无需再等
		}
		time.Sleep(100 * time.Millisecond)
	}
	return &Session{Dir: dir, Name: name}, nil
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
		_ = os.RemoveAll(s.path)
		total -= s.size
	}
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
