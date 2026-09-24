package recorder

import (
	"strings"
	"testing"
)

// realFFmpegStderr 是真实 401 失败时 ffmpeg 打到 stderr 的内容（已脱敏）。
// 降噪必须把 banner 全部丢掉，只留真正有诊断价值的行。
const realFFmpegStderr = `ffmpeg version 5.1.8-0+deb12u1 Copyright (c) 2000-2025 the FFmpeg developers
  built with gcc 12 (Debian 12.2.0-14+deb12u1)
  configuration: --prefix=/usr --extra-version=0+deb12u1 --toolchain=hardened --libdir=/usr/lib/x86_64-linux-gnu
  libavutil      57. 28.100 / 57. 28.100
  libavcodec     59. 37.100 / 59. 37.100
  libavformat    59. 27.100 / 59. 27.100
  libavdevice    59.  7.100 / 59.  7.100
  libavfilter     8. 44.100 /  8. 44.100
  libswscale      6.  7.100 /  6.  7.100
  libswresample   4.  7.100 /  4.  7.100
  libpostproc    56.  6.100 / 56.  6.100
[rtsp @ 0x5561299bcd40] method DESCRIBE failed: 401 Unauthorized
rtsp://admin:****@192.168.68.54:554/stream1: Server returned 401 Unauthorized (authorization failed)
`

// TestDistillStderr_DropsBanner 验证 banner 被丢弃、报错行被保留。
func TestDistillStderr_DropsBanner(t *testing.T) {
	got := distillStderr(realFFmpegStderr)
	if got == "" {
		t.Fatal("不应把整段都丢弃：其中含 401 报错行")
	}
	if strings.Contains(got, "libavutil") || strings.Contains(got, "configuration:") ||
		strings.Contains(got, "built with gcc") {
		t.Errorf("banner 未被丢弃: %q", got)
	}
	if !strings.Contains(got, "401") {
		t.Errorf("关键报错行丢失: %q", got)
	}
	// 行数应远小于原始（原始 12 行）
	if n := strings.Count(got, "|") + 1; n > 3 {
		t.Errorf("提炼后仍剩 %d 行，降噪不足: %q", n, got)
	}
}

// TestDistillStderr_PureBannerReturnsEmpty 纯 banner 应返回空串（不记录）。
func TestDistillStderr_PureBannerReturnsEmpty(t *testing.T) {
	pure := `ffmpeg version 5.1.8
  built with gcc 12
  configuration: --prefix=/usr
  libavutil      57. 28.100 / 57. 28.100
`
	if got := distillStderr(pure); got != "" {
		t.Errorf("纯 banner 应返回空串，实际: %q", got)
	}
}

// TestDistillStderr_RedactsCredentials 提炼时不能泄漏密码。
func TestDistillStderr_RedactsCredentials(t *testing.T) {
	raw := "rtsp://admin:SuperSecret@10.0.0.5:554/s1: Invalid data found"
	got := distillStderr(raw)
	if strings.Contains(got, "SuperSecret") {
		t.Errorf("提炼后仍含明文密码: %q", got)
	}
	if !strings.Contains(got, "****") {
		t.Errorf("应为脱敏形式: %q", got)
	}
}

// TestDistillStderr_Truncates 超长输出要截断，避免单条日志吃掉几 KB。
func TestDistillStderr_Truncates(t *testing.T) {
	long := strings.Repeat("[rtsp @ 0x1] frame decode error ", 200)
	got := distillStderr(long)
	if len(got) > errThrottleMaxLen+4 {
		t.Errorf("未截断，长度 %d", len(got))
	}
	if !strings.HasSuffix(got, "…") {
		t.Errorf("截断应有省略标记: %q", got[len(got)-10:])
	}
}

// TestNormErrKey_GroupsSimilarErrors 归一化把「同类但数字不同」的告警归为一类。
func TestNormErrKey_GroupsSimilarErrors(t *testing.T) {
	a := normErrKey("[rtsp @ 0x5561299bcd40] method DESCRIBE failed: 401 Unauthorized")
	b := normErrKey("[rtsp @ 0x7f8e12345678] method DESCRIBE failed: 401 Unauthorized")
	if a != b {
		t.Errorf("同类告警未归并:\n  a=%q\n  b=%q", a, b)
	}
	c := normErrKey("[rtsp @ 0x5561299bcd40] Connection timed out")
	if a == c {
		t.Error("不同原因不应归为同一类")
	}
}

// TestLogStderrThrottled 验证节流：同类告警在窗口内只输出一次。
func TestLogStderrThrottled(t *testing.T) {
	w := &Worker{}
	// 第一次：应记录（errKey 从空变有）
	w.logStderrThrottled("camA", realFFmpegStderr)
	if w.errKey == "" {
		t.Fatal("首次告警应被记录并建立键")
	}
	firstKey := w.errKey
	if w.errCount != 1 {
		t.Errorf("首次 errCount 应为 1，实际 %d", w.errCount)
	}
	// 同类告警连续来 5 次：只累加计数，不再输出（errKey 不变）
	for i := 0; i < 5; i++ {
		w.logStderrThrottled("camA", realFFmpegStderr)
	}
	if w.errKey != firstKey {
		t.Error("同类告警键不应变化")
	}
	if w.errCount != 6 {
		t.Errorf("应累计为 6 次，实际 %d", w.errCount)
	}
	// 换一种错误：应立刻输出并重置计数
	w.logStderrThrottled("camA", "[rtsp @ 0x1] Connection timed out\n")
	if w.errKey == firstKey {
		t.Error("不同错误应更新键")
	}
	if w.errCount != 1 {
		t.Errorf("换类后计数应重置为 1，实际 %d", w.errCount)
	}
	// 纯 banner 不产生任何状态变化（不应被当成一次告警）
	before := w.errKey
	w.logStderrThrottled("camA", "ffmpeg version 5.1.8\n  libavutil 57\n")
	if w.errKey != before || w.errCount != 1 {
		t.Error("纯 banner 不应改变告警状态")
	}
}
