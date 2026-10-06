package recorder

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"cyannvr/server/config"
)

// TestIsFutureDateDir 覆盖日期目录判定：今天/明天必须保护，昨天/非日期名不保护。
func TestIsFutureDateDir(t *testing.T) {
	now := time.Date(2026, 10, 7, 1, 30, 0, 0, time.Local)
	cases := []struct {
		name string
		want bool
	}{
		{"20261007", true},  // 今天：必须保护（当天还要继续写）
		{"20261008", true},  // 明天：跨午夜要用，删了就静默中断
		{"20261009", true},  // 后天
		{"20261006", false}, // 昨天：可以回收
		{"20260901", false}, // 更早
		{"", false},
		{"2026100", false},   // 长度不足
		{"202610071", false}, // 长度超长
		{"abcdefgh", false},  // 非数字
		{"device-id", false}, // 设备目录名不会被误判
	}
	for _, c := range cases {
		if got := isFutureDateDir(c.name, now); got != c.want {
			t.Errorf("isFutureDateDir(%q) = %v, want %v", c.name, got, c.want)
		}
	}
}

// TestPruneEmptyDirsKeepsTodayAndFuture 是本次修复的核心回归：
// 空的「今天/明天/后天」目录必须保留，空的过去目录必须删除。
//
// 回滚修复（去掉 isFutureDateDir 判断）后此测试会失败——正是这个缺陷
// 曾在生产造成 94 分钟录像静默中断。
func TestPruneEmptyDirsKeepsTodayAndFuture(t *testing.T) {
	root := t.TempDir()
	dev := filepath.Join(root, "dev-1")
	now := time.Now()

	// 今天的日期名按本地时区生成，与实现保持一致
	today := now.Format("20060102")
	tomorrow := now.AddDate(0, 0, 1).Format("20060102")
	dayAfter := now.AddDate(0, 0, 2).Format("20060102")
	yesterday := now.AddDate(0, 0, -1).Format("20060102")
	lastMonth := now.AddDate(0, 0, -30).Format("20060102")

	for _, d := range []string{today, tomorrow, dayAfter, yesterday, lastMonth} {
		if err := os.MkdirAll(filepath.Join(dev, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	m := &Manager{cfg: &config.Config{RecordDir: root}}
	m.pruneEmptyDirs(root, 0)

	for _, d := range []string{today, tomorrow, dayAfter} {
		if _, err := os.Stat(filepath.Join(dev, d)); err != nil {
			t.Errorf("日期目录 %s 被误删（今天/未来必须保留）: %v", d, err)
		}
	}
	for _, d := range []string{yesterday, lastMonth} {
		if _, err := os.Stat(filepath.Join(dev, d)); !os.IsNotExist(err) {
			t.Errorf("过去的空目录 %s 应被清理, stat err = %v", d, err)
		}
	}
}

// TestPruneEmptyDirsStillRemovesPastWithContent 确保保护逻辑不误伤：
// 过去目录即使非空也不会被删；非日期命名的空目录仍会被回收。
func TestPruneEmptyDirsStillRemovesPastWithContent(t *testing.T) {
	root := t.TempDir()
	dev := filepath.Join(root, "dev-1")
	old := time.Now().AddDate(0, 0, -10).Format("20060102")
	stale := filepath.Join(dev, "not-a-date")
	for _, d := range []string{filepath.Join(dev, old), stale} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// 过去目录里放一个文件，应保留
	keep := filepath.Join(dev, old, "120000.mp4")
	if err := os.WriteFile(keep, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	m := &Manager{cfg: &config.Config{RecordDir: root}}
	m.pruneEmptyDirs(root, 0)

	if _, err := os.Stat(keep); err != nil {
		t.Errorf("非空目录内容被误删: %v", err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Errorf("非日期命名的空目录应被回收, stat err = %v", err)
	}
}

// TestSegmentWriteFailure 覆盖「静默中断」的错误识别。
//
// 这些字符串取自真实 ffmpeg 输出（tee + segment 目标目录不存在时）。
func TestSegmentWriteFailure(t *testing.T) {
	real := "[segment @ 0x55caeb12a980] Failed to open segment 'out/20261007/014138.mp4'\n" +
		"Could not write header for output file #0 (incorrect codec parameters ?): No such file or directory\n"
	if !segmentWriteFailure(real) {
		t.Error("应识别出真实的分段写入失败输出")
	}
	real2 := "[tee @ 0x55c23e5a2580] Slave '[f=segment:...]': error writing header: No such file or directory"
	if !segmentWriteFailure(real2) {
		t.Error("应识别 tee 的 error writing header")
	}
	benign := "frame=  100 fps=25 q=28.0 size=1024kB time=00:00:04.00 bitrate=2097.2kbits/s speed=1x"
	if segmentWriteFailure(benign) {
		t.Error("正常进度输出不应被误判为失败")
	}
	if segmentWriteFailure("") {
		t.Error("空输出不应被误判")
	}
}

// TestSegmentFailureSinceFingerprint 确保同一错误只触发一次（指纹去重），
// 避免每次巡检都重启进程。
func TestSegmentFailureSinceFingerprint(t *testing.T) {
	log1 := "[segment @ 0x1] Failed to open segment 'a/20261008/000000.mp4'"
	fp1, ok1 := segmentFailureSince(log1)
	if !ok1 || fp1 == "" {
		t.Fatal("应提取到错误指纹")
	}
	// 同一行重复出现 → 指纹相同（调用方据此去重）
	fp2, _ := segmentFailureSince(log1 + "\n" + log1)
	if fp1 != fp2 {
		t.Errorf("同一错误应得到相同指纹: %q vs %q", fp1, fp2)
	}
	// 不同错误的指纹必须不同
	fp3, _ := segmentFailureSince("[segment @ 0x2] Failed to open segment 'b/20261009/010101.mp4'")
	if fp3 == fp1 {
		t.Error("不同错误行的指纹不应相同（否则第二次故障会被漏掉）")
	}
	if _, ok := segmentFailureSince("nothing wrong here"); ok {
		t.Error("正常输出不应返回指纹")
	}
}
