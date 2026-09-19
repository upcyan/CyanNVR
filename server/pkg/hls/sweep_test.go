package hls

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"simplenvr/server/config"
)

// newTestHls 构造一个只依赖临时目录的 Hls 实例（不启动 janitor）。
func newTestHls(t *testing.T) (*Hls, string) {
	t.Helper()
	dir := t.TempDir()
	return &Hls{cfg: &config.Config{LiveDir: dir}}, dir
}

// writeSession 在 playback 下建一个会话目录并写入一个分片文件。
func writeSession(t *testing.T, liveDir, name string) string {
	t.Helper()
	d := filepath.Join(liveDir, "playback", name)
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(d, "seg0.ts"), []byte("abcdef"), 0o644); err != nil {
		t.Fatal(err)
	}
	return d
}

// TestSweepRemovesExpiredSessions 验证过期回放会话被回收、未过期的被保留。
//
// 回归背景：原实现只在有人发起回放时才顺带清理，且 TTL 长达 2 小时，
// 于是「看完一个片段就留下一个 12-25MB 目录」，实测堆积 10 个目录共 332MB。
func TestSweepRemovesExpiredSessions(t *testing.T) {
	h, liveDir := newTestHls(t)
	oldDir := writeSession(t, liveDir, "expired")
	newDir := writeSession(t, liveDir, "active")

	// 把过期会话的修改时间推回到 TTL 之前
	past := time.Now().Add(-playbackTTL - time.Minute)
	if err := os.Chtimes(oldDir, past, past); err != nil {
		t.Fatal(err)
	}

	h.sweep()

	if _, err := os.Stat(oldDir); !os.IsNotExist(err) {
		t.Errorf("过期会话目录应被回收，实际仍存在 (err=%v)", err)
	}
	if _, err := os.Stat(newDir); err != nil {
		t.Errorf("未过期会话目录被误删: %v", err)
	}
}

// TestSweepMissingDir 验证 playback 目录尚不存在时 sweep 不 panic。
func TestSweepMissingDir(t *testing.T) {
	h, _ := newTestHls(t)
	h.sweep()
}

// TestSweepSkipsFiles 验证 playback 下的普通文件不会被误删。
func TestSweepSkipsFiles(t *testing.T) {
	h, liveDir := newTestHls(t)
	if err := os.MkdirAll(filepath.Join(liveDir, "playback"), 0o755); err != nil {
		t.Fatal(err)
	}
	f := filepath.Join(liveDir, "playback", "keep.txt")
	if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	past := time.Now().Add(-playbackTTL - time.Minute)
	if err := os.Chtimes(f, past, past); err != nil {
		t.Fatal(err)
	}

	h.sweep()

	if _, err := os.Stat(f); err != nil {
		t.Errorf("普通文件不应被回收: %v", err)
	}
}

// TestDirSize 验证目录占用统计（含子目录）。
func TestDirSize(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a"), []byte("12345"), 0o644); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(dir, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "b"), []byte("123"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := dirSize(dir); got != 8 {
		t.Errorf("dirSize = %d, want 8", got)
	}
}
