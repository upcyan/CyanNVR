package recorder

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"cyannvr/server/models"
	"cyannvr/server/pkg/ai"
)

// testDevice 造一个最小可用的设备对象（只填测试关心的字段）。
func testDevice() *models.Device {
	return &models.Device{ID: "dev1", Name: "测试机", Source: models.SourceRTSP}
}

// TestGrabSnapshotReusesFreshFile 新鲜文件应直接复用，不起 ffmpeg。
// 用一个「假」worker（无 recordProc）来证明：若走了抓帧路径必然失败，
// 而成功返回即说明它只读了缓存。
func TestGrabSnapshotReusesFreshFile(t *testing.T) {
	dir := t.TempDir()
	snapDir := filepath.Join(dir, "snap")
	if err := os.MkdirAll(snapDir, 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(snapDir, "current.jpg")
	if err := os.WriteFile(target, []byte("fake-jpeg"), 0o644); err != nil {
		t.Fatal(err)
	}

	m := &Manager{cfg: newTestConfig(dir), workers: map[string]*Worker{}}
	// 注册一个「录像未运行」的 worker：抓帧路径会直接报错
	m.workers["dev1"] = &Worker{mgr: m, snapDir: snapDir, recordProc: nil}

	got, err := m.GrabSnapshot("dev1", 3*time.Second)
	if err != nil {
		t.Fatalf("新鲜文件应被复用，却报错: %v", err)
	}
	if got != target {
		t.Errorf("路径不符: got %q want %q", got, target)
	}
}

// TestGrabSnapshotStaleTriggersCapture 过期文件必须触发抓帧；
// 录像管线未运行时抓帧失败，用于证明「确实尝试了抓帧」而不是继续用旧图。
func TestGrabSnapshotStaleTriggersCapture(t *testing.T) {
	dir := t.TempDir()
	snapDir := filepath.Join(dir, "snap")
	if err := os.MkdirAll(snapDir, 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(snapDir, "current.jpg")
	if err := os.WriteFile(target, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	// 把 mtime 设为 1 小时前 → 超过 maxAge
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(target, old, old); err != nil {
		t.Fatal(err)
	}

	m := &Manager{cfg: newTestConfig(dir), workers: map[string]*Worker{}}
	m.workers["dev1"] = &Worker{mgr: m, snapDir: snapDir}

	_, err := m.GrabSnapshot("dev1", 3*time.Second)
	if err == nil {
		t.Error("过期文件应触发抓帧；录像未运行时抓帧必失败，不应返回成功")
	}
}

// TestGrabSnapshotUnknownDevice 未知设备应明确报错，而不是假装成功。
func TestGrabSnapshotUnknownDevice(t *testing.T) {
	m := &Manager{cfg: newTestConfig(t.TempDir()), workers: map[string]*Worker{}}
	if _, err := m.GrabSnapshot("nonexistent", time.Second); err == nil {
		t.Error("未知设备应返回错误")
	}
}

// TestGrabSnapshotConcurrentDedup 并发去重是本功能的核心：
// 同一设备并发请求只应产生一次实际抓帧。
//
// 验证方式：把 captureFrame 的调用计数间接暴露出来——这里用
// snapInflight 的合并语义验证：并发 N 个调用，全部拿到同一结果
// （要么全成功、要么全失败），且进行中的调用数从未超过 1。
func TestGrabSnapshotConcurrentDedup(t *testing.T) {
	dir := t.TempDir()
	snapDir := filepath.Join(dir, "snap")
	if err := os.MkdirAll(snapDir, 0o755); err != nil {
		t.Fatal(err)
	}
	m := &Manager{cfg: newTestConfig(dir), workers: map[string]*Worker{}}
	m.workers["dev1"] = &Worker{mgr: m, snapDir: snapDir}

	const N = 12
	var wg sync.WaitGroup
	results := make([]error, N)
	// 监控 snapInflight 的实时条目数（同一设备最多 1 个）
	maxInflight := 0
	var mu sync.Mutex
	stop := make(chan struct{})
	go func() {
		for {
			select {
			case <-stop:
				return
			default:
			}
			cnt := 0
			snapInflight.Range(func(_, _ any) bool { cnt++; return true })
			mu.Lock()
			if cnt > maxInflight {
				maxInflight = cnt
			}
			mu.Unlock()
			time.Sleep(2 * time.Millisecond)
		}
	}()

	start := make(chan struct{})
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			<-start                                     // 尽量同时发起，制造并发
			_, results[idx] = m.GrabSnapshot("dev1", 0) // maxAge=0 强制走抓帧路径
		}(i)
	}
	close(start)
	wg.Wait()
	close(stop)

	// 全部失败（录像未运行）是预期的；关键是「结果一致」而非部分成功
	for i, e := range results {
		if e == nil {
			t.Errorf("第 %d 个调用意外成功（录像未运行）", i)
		}
	}
	mu.Lock()
	peak := maxInflight
	mu.Unlock()
	if peak > 1 {
		t.Errorf("并发去重失效：同时在飞行的抓帧数达到 %d（应为 1）", peak)
	}
	// 结束后必须清空，否则下次抓拍会被永久阻塞
	left := 0
	snapInflight.Range(func(_, _ any) bool { left++; return true })
	if left != 0 {
		t.Errorf("抓帧结束后 snapInflight 应清空，残留 %d 项", left)
	}
}

// TestSnapshotPath 未知设备返回约定路径（不 panic）。
func TestSnapshotPath(t *testing.T) {
	dir := t.TempDir()
	m := &Manager{cfg: newTestConfig(dir), workers: map[string]*Worker{}}
	got := m.SnapshotPath("ghost")
	// 未知设备回退到 cfg.SnapDir（生产由 NVR_SNAPS 派生），保持与 worker 内一致的布局
	want := filepath.Join(dir, "snapshots", "ghost", "current.jpg")
	if got != want {
		t.Errorf("SnapshotPath = %q, want %q", got, want)
	}
}

// TestNeedSnapshotsGate 常驻抓帧只在 AI 开启时需要。
func TestNeedSnapshotsGate(t *testing.T) {
	tr, fa := true, false
	cases := []struct {
		name   string
		devOn  *bool
		global bool
		hasAI  bool
		want   bool
	}{
		{"无 AI 分析器 → 不需要", nil, true, false, false},
		{"全局开、设备未设 → 需要", nil, true, true, true},
		{"全局关、设备未设 → 不需要", nil, false, true, false},
		{"设备显式开（覆盖全局关）→ 需要", &tr, false, true, true},
		{"设备显式关（覆盖全局开）→ 不需要", &fa, true, true, false},
	}
	for _, c := range cases {
		m := &Manager{cfg: newTestConfig(t.TempDir())}
		m.cfg.AIEnabled = c.global
		if c.hasAI {
			// needSnapshots 只判 ai 是否为 nil，零值 Analyzer 足够
			m.ai = &ai.Analyzer{}
		}
		w := &Worker{mgr: m, dev: testDevice()}
		w.dev.AIEnabled = c.devOn
		if got := w.needSnapshots(); got != c.want {
			t.Errorf("%s: needSnapshots = %v, want %v", c.name, got, c.want)
		}
	}
}
