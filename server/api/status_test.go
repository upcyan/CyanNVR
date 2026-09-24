//go:build !windows

package api

import "testing"

// TestDiskSnapshotFields 验证磁盘快照返回界面需要的字段，
// 且 usedPct 计算合理（0~100）。
func TestDiskSnapshotFields(t *testing.T) {
	// 用当前工作目录当数据目录，确保 Statfs 能成功
	s := &Server{cfg: newStatusTestConfig(t.TempDir())}
	d := s.diskSnapshot()
	if _, ok := d["error"]; ok {
		t.Fatalf("diskSnapshot 返回错误: %v", d["error"])
	}
	for _, k := range []string{"totalGB", "usedGB", "freeGB", "usedPct", "path", "minFreeMB", "low"} {
		if _, ok := d[k]; !ok {
			t.Errorf("缺少字段 %q", k)
		}
	}
	pct, ok := d["usedPct"].(float64)
	if !ok {
		t.Fatalf("usedPct 类型应为 float64，实际 %T", d["usedPct"])
	}
	if pct < 0 || pct > 100 {
		t.Errorf("usedPct 越界: %v", pct)
	}
}

// TestProcessStats 验证进程统计能读到内存与线程数。
func TestProcessStats(t *testing.T) {
	p := processStats()
	if _, ok := p["goroutines"]; !ok {
		t.Error("缺少 goroutines")
	}
	mem, ok := p["memMB"].(float64)
	if !ok {
		t.Fatal("缺少 memMB（应能从 /proc/self/statm 读到）")
	}
	if mem <= 0 {
		t.Errorf("memMB 应为正数，实际 %v", mem)
	}
	if up, ok := p["uptimeSec"].(int64); ok && up < 0 {
		t.Errorf("uptimeSec 不应为负: %v", up)
	}
}

// TestCPUPercentNeedsTwoSamples 首次采样只建基准，第二次才能给出数值。
func TestCPUPercentNeedsTwoSamples(t *testing.T) {
	// 重置全局采样状态
	cpuSample.valid = false
	if _, ok := cpuPercentSinceLastSample(); ok {
		t.Error("首次调用不应返回 CPU 值（只建基准）")
	}
	// 制造一点 CPU 消耗
	sum := 0
	for i := 0; i < 200000; i++ {
		sum += i
	}
	_ = sum
	pct, ok := cpuPercentSinceLastSample()
	if !ok {
		t.Fatal("第二次调用应能算出 CPU")
	}
	if pct < 0 {
		t.Errorf("CPU 百分比不应为负: %v", pct)
	}
}

// TestReadSelfJiffies 能读到自身 CPU jiffies。
func TestReadSelfJiffies(t *testing.T) {
	a, ok := readSelfJiffies()
	if !ok {
		t.Fatal("读取自身 jiffies 失败")
	}
	sum := 0
	for i := 0; i < 500000; i++ {
		sum += i
	}
	_ = sum
	b, ok := readSelfJiffies()
	if !ok {
		t.Fatal("二次读取失败")
	}
	if b < a {
		t.Errorf("jiffies 不应倒退: %d → %d", a, b)
	}
}

// TestProcessUptime 进程运行时长应 >= 0 且小于系统运行时长太多不合理。
func TestProcessUptime(t *testing.T) {
	up, ok := processUptimeSeconds()
	if !ok {
		t.Skip("当前环境无法读取 /proc/uptime")
	}
	if up < 0 {
		t.Errorf("运行时长不应为负: %d", up)
	}
}
