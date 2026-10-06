package recorder

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"cyannvr/server/config"
	"cyannvr/server/models"
	"cyannvr/server/store"
)

// TestCleanupKeepsTodayAndTomorrowDirs 是生产事故的端到端回归：
// 清理循环跑完后，每台设备的「今天/明天/后天」目录必须仍然存在。
//
// 事故经过：pruneEmptyDirs 把明天的空目录当垃圾删掉 → 跨过 00:00 后
// ffmpeg 写不进分段（segment muxer 不会自建目录）→ 因 tee 的 segment 是
// 子输出，进程不退出、直播继续写，假死看门狗也判正常 → 录像静默中断 94 分钟。
func TestCleanupKeepsTodayAndTomorrowDirs(t *testing.T) {
	root := t.TempDir()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()

	devices := []string{"dev-1", "dev-2"}
	for _, id := range devices {
		if err := st.CreateDevice(models.Device{ID: id, Name: id, Source: models.SourceRTSP, Created: time.Now()}); err != nil {
			t.Fatalf("seed device %s: %v", id, err)
		}
	}
	recordDir := filepath.Join(root, "recordings")
	m := &Manager{cfg: &config.Config{RecordDir: recordDir, RetentionDays: 30}, st: st}

	now := time.Now()
	// 先造出「过去的空目录」——清理应该回收它们
	for _, id := range devices {
		for _, off := range []int{-1, -30} {
			d := filepath.Join(recordDir, id, now.AddDate(0, 0, off).Format("20060102"))
			if err := os.MkdirAll(d, 0o755); err != nil {
				t.Fatal(err)
			}
		}
	}

	// 驱动真正的生产入口 cleanupOldRecordings（含 prune 与日期目录兜底），
	// 而不是分别调用内部函数——否则「接线漏掉」这类缺陷测不出来。
	m.cleanupOldRecordings()

	// 核心断言：今天/明天/后天目录必须存在
	for _, id := range devices {
		for _, off := range []int{0, 1, 2} {
			day := now.AddDate(0, 0, off).Format("20060102")
			p := filepath.Join(recordDir, id, day)
			fi, err := os.Stat(p)
			if err != nil {
				t.Fatalf("设备 %s 的日期目录 %s 不存在: %v（跨午夜后录像会静默中断）", id, day, err)
			}
			if !fi.IsDir() {
				t.Fatalf("%s 不是目录", p)
			}
		}
		// 过去的空目录应被回收
		old := filepath.Join(recordDir, id, now.AddDate(0, 0, -30).Format("20060102"))
		if _, err := os.Stat(old); !os.IsNotExist(err) {
			t.Errorf("过去的空目录 %s 应被清理, stat err = %v", old, err)
		}
	}
}

// TestEnsureRecordDayDirsRecreatesAfterPrune 直接覆盖「被删掉后能补回来」，
// 这正是跨天场景所需的自我修复能力。
func TestEnsureRecordDayDirsRecreatesAfterPrune(t *testing.T) {
	root := t.TempDir()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()
	if err := st.CreateDevice(models.Device{ID: "dev-1", Name: "cam", Source: models.SourceRTSP, Created: time.Now()}); err != nil {
		t.Fatal(err)
	}
	recordDir := filepath.Join(root, "recordings")
	m := &Manager{cfg: &config.Config{RecordDir: recordDir}, st: st}

	now := time.Now()
	tomorrow := now.AddDate(0, 0, 1).Format("20060102")

	m.ensureRecordDayDirs(now)
	if _, err := os.Stat(filepath.Join(recordDir, "dev-1", tomorrow)); err != nil {
		t.Fatalf("明天目录未创建: %v", err)
	}

	// 人为删掉（模拟旧版 prune 的误删），兜底逻辑必须能补回来
	if err := os.RemoveAll(filepath.Join(recordDir, "dev-1", tomorrow)); err != nil {
		t.Fatal(err)
	}
	m.ensureRecordDayDirs(now)
	if _, err := os.Stat(filepath.Join(recordDir, "dev-1", tomorrow)); err != nil {
		t.Fatalf("明天目录未被补回: %v", err)
	}
}
