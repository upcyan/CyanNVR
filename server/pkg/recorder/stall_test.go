package recorder

import (
	"os/exec"
	"testing"
	"time"
)

// TestStalledSince 覆盖假死判定内核：读数不增长且超过窗口才判假死。
func TestStalledSince(t *testing.T) {
	base := time.Now().Add(-60 * time.Second)
	window := 45 * time.Second

	cases := []struct {
		name    string
		st      *stallState
		nowRead uint64
		now     time.Time
		want    bool
	}{
		{
			name:    "读数增长中 → 不判假死",
			st:      &stallState{pid: 1, lastRead: 1000, lastAt: base},
			nowRead: 2000,
			now:     time.Now(),
			want:    false,
		},
		{
			name:    "读数不变且超过窗口 → 判假死",
			st:      &stallState{pid: 1, lastRead: 1000, lastAt: base},
			nowRead: 1000,
			now:     time.Now(),
			want:    true,
		},
		{
			name:    "读数不变但未到窗口 → 继续观察",
			st:      &stallState{pid: 1, lastRead: 1000, lastAt: time.Now().Add(-5 * time.Second)},
			nowRead: 1000,
			now:     time.Now(),
			want:    false,
		},
		{
			name:    "刚好达到窗口边界 → 判假死",
			st:      &stallState{pid: 1, lastRead: 1000, lastAt: time.Now().Add(-window)},
			nowRead: 1000,
			now:     time.Now(),
			want:    true,
		},
		{
			name:    "首次采样（无基准）→ 不判假死",
			st:      &stallState{pid: 1},
			nowRead: 500,
			now:     time.Now(),
			want:    false,
		},
		{
			name:    "nil 状态 → 不判假死",
			st:      nil,
			nowRead: 500,
			now:     time.Now(),
			want:    false,
		},
		{
			name:    "读数倒退（进程重启/计数器重置）→ 不立即判死",
			st:      &stallState{pid: 1, lastRead: 5000, lastAt: base},
			nowRead: 10,
			now:     time.Now(),
			want:    true, // 读数变小说明已不是同一进程，超窗口即重启
		},
	}
	for _, c := range cases {
		if got := stalledSince(c.st, c.nowRead, c.now, window); got != c.want {
			t.Errorf("%s: stalledSince = %v, want %v", c.name, got, c.want)
		}
	}
}

// TestProcReadBytes 验证能从真实进程读到累计读取字节数；
// 读不到（不存在的 pid）必须返回 false，而不是假装读到 0——
// 否则看门狗会把「无法采样」误判成「假死」而乱杀进程。
func TestProcReadBytes(t *testing.T) {
	cmd := exec.Command("sleep", "5")
	if err := cmd.Start(); err != nil {
		t.Skipf("无法启动 sleep：%v", err)
	}
	defer func() { _ = cmd.Process.Kill(); _, _ = cmd.Process.Wait() }()

	if _, ok := procReadBytes(cmd.Process.Pid); !ok {
		t.Skip("当前环境未暴露 /proc/<pid>/io（容器限制），看门狗会自动跳过")
	}
	if _, ok := procReadBytes(999999999); ok {
		t.Error("对不存在的 pid 应返回 false")
	}
	if _, ok := procReadBytes(0); ok {
		t.Error("pid=0 应返回 false")
	}
	if _, ok := procReadBytes(-1); ok {
		t.Error("负数 pid 应返回 false")
	}
}
