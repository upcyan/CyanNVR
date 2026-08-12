package hls

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"simplenvr/server/config"
	"simplenvr/server/pkg/ffmpeg"
	"simplenvr/server/store"
)

type Hls struct {
	cfg *config.Config
	st  *store.Store
}

func New(cfg *config.Config, st *store.Store) *Hls {
	return &Hls{cfg: cfg, st: st}
}

// Session describes a generated playback HLS session directory.
type Session struct {
	Dir  string
	Name string
}

// CreatePlayback builds a continuous HLS from recorded segments covering [start, end].
func (h *Hls) CreatePlayback(deviceID string, start, end time.Time, transcode bool) (*Session, error) {
	h.cleanupSessions()
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
	args := []string{
		"-loglevel", "error", "-y",
		"-f", "concat", "-safe", "0", "-i", listPath,
		"-ss", fmt.Sprintf("%.3f", rel),
		"-t", fmt.Sprintf("%.3f", dur),
		"-an",
	}
	if transcode {
		args = append(args, "-c:v", "libx264", "-preset", "veryfast", "-pix_fmt", "yuv420p", "-g", "30")
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
	return &Session{Dir: dir, Name: name}, nil
}

func (h *Hls) cleanupSessions() {
	base := filepath.Join(h.cfg.LiveDir, "playback")
	entries, err := os.ReadDir(base)
	if err != nil {
		return
	}
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			continue
		}
		if time.Since(info.ModTime()) > 2*time.Hour {
			_ = os.RemoveAll(filepath.Join(base, e.Name()))
		}
	}
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
