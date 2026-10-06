package store

import (
	"path/filepath"
	"testing"
	"time"
)

// TestSegmentsForDayEmptyDayNotNull 回归：无录像的日期 SegmentsForDay 曾返回
// nil slice，经 gin 序列化成 {"segments":null}，前端对 null 调 .map 抛
// TypeError，把「当日无录像」误报成「录像加载失败」。必须返回非 nil 空切片。
func TestSegmentsForDayEmptyDayNotNull(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("打开测试库失败: %v", err)
	}
	defer st.Close()

	// 空库：任意日期都无录像
	day := time.Date(2026, 10, 1, 0, 0, 0, 0, time.Local)
	segs, err := st.SegmentsForDay("no-such-device", day, day.AddDate(0, 0, 1))
	if err != nil {
		t.Fatalf("SegmentsForDay 出错: %v", err)
	}
	if segs == nil {
		t.Fatal("无录像日期必须返回空切片而非 nil（nil 会被序列化成 JSON null）")
	}
	if len(segs) != 0 {
		t.Fatalf("期望 0 段，实际 %d", len(segs))
	}

	after, err := st.SegmentsAfter("no-such-device", day)
	if err != nil {
		t.Fatalf("SegmentsAfter 出错: %v", err)
	}
	if after == nil {
		t.Fatal("SegmentsAfter 同样必须返回空切片而非 nil")
	}
}
