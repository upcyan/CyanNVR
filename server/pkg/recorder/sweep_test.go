package recorder

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"cyannvr/server/config"
)

// newTestConfig 构造测试用配置。
// SnapDir 与生产一致地由 dataDir 派生，避免测试与线上行为偏差。
func newTestConfig(recordDir string) *config.Config {
	return &config.Config{
		DataDir:   recordDir,
		RecordDir: recordDir,
		SnapDir:   filepath.Join(recordDir, "snapshots"),
		LiveDir:   filepath.Join(recordDir, "live"),
		MinFreeMB: 0,
	}
}

// makeFile 造一个指定大小的文件，用 head 字节开头，可选在尾部追加标记。
func makeFile(t *testing.T, path string, head []byte, sizeKB int, tail []byte, mtime time.Time) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write(head); err != nil {
		t.Fatal(err)
	}
	if sizeKB > 0 {
		pad := make([]byte, 1024)
		for i := 0; i < sizeKB; i++ {
			if _, err := f.Write(pad); err != nil {
				t.Fatal(err)
			}
		}
	}
	if len(tail) > 0 {
		if _, err := f.Write(tail); err != nil {
			t.Fatal(err)
		}
	}
	f.Close()
	if !mtime.IsZero() {
		if err := os.Chtimes(path, mtime, mtime); err != nil {
			t.Fatal(err)
		}
	}
}

// TestSweepBrokenSegments 覆盖损坏尾段清理的关键判定：
// 该删的（无 moov 的旧文件、0 字节）删掉；
// 不该删的（正在写的新文件、moov 在尾部的完好文件）必须保留。
func TestSweepBrokenSegments(t *testing.T) {
	root := t.TempDir()
	day := filepath.Join(root, "dev1", "20260924")
	old := time.Now().Add(-10 * time.Minute) // 在 [3min,24h) 清理窗口内
	fresh := time.Now().Add(-30 * time.Second)

	ftyp := []byte{0x00, 0x00, 0x00, 0x20, 'f', 't', 'y', 'p', 'i', 's', 'o', 'm'}
	moovHead := append(append([]byte{}, ftyp...), []byte("moov")...)
	mdatHead := append(append([]byte{}, ftyp...), []byte("mdat")...)

	okPath := filepath.Join(day, "head_moov.mp4")
	tailPath := filepath.Join(day, "tail_moov.mp4")
	brokenPath := filepath.Join(day, "broken.mp4")
	recordingPath := filepath.Join(day, "recording.mp4")
	zeroPath := filepath.Join(day, "zero.mp4")
	tooOldPath := filepath.Join(day, "ancient_broken.mp4")
	txtPath := filepath.Join(day, "note.txt")

	makeFile(t, okPath, moovHead, 200, nil, old)
	makeFile(t, tailPath, mdatHead, 100, []byte("moov"), old)
	makeFile(t, brokenPath, mdatHead, 50, nil, old)
	makeFile(t, recordingPath, mdatHead, 50, nil, fresh) // 正在写
	makeFile(t, zeroPath, nil, 0, nil, old)
	makeFile(t, tooOldPath, mdatHead, 50, nil, time.Now().Add(-30*time.Hour)) // 超出窗口
	makeFile(t, txtPath, []byte("not a video"), 1, nil, old)

	m := &Manager{cfg: newTestConfig(root)}
	m.sweepBrokenSegments()

	exists := func(p string) bool { _, err := os.Stat(p); return err == nil }
	cases := []struct {
		name   string
		path   string
		expect bool // true = 应保留
	}{
		{"头部含 moov 的完好文件", okPath, true},
		{"尾部含 moov 的完好文件", tailPath, true},
		{"无 moov 的旧损坏文件", brokenPath, false},
		{"正在写的新文件（无 moov）", recordingPath, true},
		{"0 字节文件", zeroPath, false},
		{"超出清理窗口的旧文件", tooOldPath, true},
		{"非 mp4 文件", txtPath, true},
	}
	for _, c := range cases {
		if got := exists(c.path); got != c.expect {
			t.Errorf("%s: 保留=%v 期望=%v (%s)", c.name, got, c.expect, filepath.Base(c.path))
		}
	}
}

// TestPruneEmptyDirs 验证空目录回收：空目录被删，非空目录与其父链保留。
func TestPruneEmptyDirs(t *testing.T) {
	root := t.TempDir()
	// dev1/20260924 有文件 → 整条链保留
	keepDir := filepath.Join(root, "dev1", "20260924")
	if err := os.MkdirAll(keepDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(keepDir, "a.mp4"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	// dev1/20260925 空 → 删；dev1 本身非空 → 保留
	emptySibling := filepath.Join(root, "dev1", "20260925")
	// dev2/20260923 空 → 连 dev2 一起删
	nestedEmpty := filepath.Join(root, "dev2", "20260923")
	// playback 目录含空子目录 → 也应被清
	pbEmpty := filepath.Join(root, "playback", "sess1")
	for _, d := range []string{emptySibling, nestedEmpty, pbEmpty} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	mgr := &Manager{cfg: newTestConfig(root)}
	mgr.pruneEmptyDirs(root, 0)

	exists := func(p string) bool { _, err := os.Stat(p); return err == nil }
	if !exists(keepDir) {
		t.Error("含文件的目录被误删")
	}
	if exists(emptySibling) {
		t.Error("空目录未清理")
	}
	if exists(nestedEmpty) || exists(filepath.Join(root, "dev2")) {
		t.Error("空目录链未完全清理")
	}
	if exists(pbEmpty) {
		t.Error("playback 下的空会话目录未清理")
	}
	if !exists(root) {
		t.Error("根目录被误删")
	}
}
