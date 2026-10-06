package api

// selectExportCuts 的单元测试：裁剪点计算是导出正确性的核心。

import (
	"testing"
	"time"

	"cyannvr/server/models"
)

func seg(id string, start, end time.Time) models.RecordingSegment {
	return models.RecordingSegment{ID: id, Start: start, End: end, Path: "/rec/" + id + ".mp4"}
}

func base() time.Time {
	return time.Date(2026, 10, 7, 0, 0, 0, 0, time.Local)
}

func TestSelectExportCuts_SingleSegmentInside(t *testing.T) {
	// 段 00:45-01:15 完全落在导出范围 00:30-01:30 内：不裁剪
	s := seg("a", base().Add(45*time.Minute), base().Add(75*time.Minute))
	cuts := selectExportCuts([]models.RecordingSegment{s}, base().Add(30*time.Minute), base().Add(90*time.Minute))
	if len(cuts) != 1 {
		t.Fatalf("cuts=%d want 1", len(cuts))
	}
	if cuts[0].Inpoint != 0 || cuts[0].Outpoint != 0 {
		t.Fatalf("in=%d out=%d want 0/0（段完全在范围内）", cuts[0].Inpoint, cuts[0].Outpoint)
	}
}

func TestSelectExportCuts_ClipStartAndEnd(t *testing.T) {
	// 段 00:00-02:00，导出 00:30-01:30 → in=30m out=90m（微秒）
	s := seg("a", base(), base().Add(2*time.Hour))
	cuts := selectExportCuts([]models.RecordingSegment{s}, base().Add(30*time.Minute), base().Add(90*time.Minute))
	if len(cuts) != 1 {
		t.Fatalf("cuts=%d want 1", len(cuts))
	}
	if got, want := cuts[0].Inpoint, (30 * time.Minute).Microseconds(); got != want {
		t.Fatalf("inpoint=%d want %d", got, want)
	}
	if got, want := cuts[0].Outpoint, (90 * time.Minute).Microseconds(); got != want {
		t.Fatalf("outpoint=%d want %d", got, want)
	}
}

func TestSelectExportCuts_SpanTwoSegments(t *testing.T) {
	a := seg("a", base(), base().Add(time.Hour))              // 00:00-01:00
	b := seg("b", base().Add(time.Hour), base().Add(2*time.Hour)) // 01:00-02:00
	cuts := selectExportCuts([]models.RecordingSegment{b, a}, base().Add(30*time.Minute), base().Add(90*time.Minute))
	if len(cuts) != 2 {
		t.Fatalf("cuts=%d want 2", len(cuts))
	}
	if cuts[0].Path != a.Path {
		t.Fatalf("cuts 未按开始时间排序: %s", cuts[0].Path)
	}
	if cuts[0].Inpoint != (30 * time.Minute).Microseconds() || cuts[0].Outpoint != 0 {
		t.Fatalf("a 裁剪错误: %+v", cuts[0])
	}
	if cuts[1].Inpoint != 0 || cuts[1].Outpoint != (30 * time.Minute).Microseconds() {
		t.Fatalf("b 裁剪错误: %+v", cuts[1])
	}
}

func TestSelectExportCuts_NonIntersectSkipped(t *testing.T) {
	early := seg("early", base(), base().Add(time.Hour))
	late := seg("late", base().Add(3*time.Hour), base().Add(4*time.Hour))
	mid := seg("mid", base().Add(time.Hour), base().Add(2*time.Hour))
	// 导出 01:00-02:00：early（00-01）端点相触不算相交，late 不相交
	cuts := selectExportCuts([]models.RecordingSegment{early, late, mid}, base().Add(time.Hour), base().Add(2*time.Hour))
	if len(cuts) != 1 || cuts[0].Path != mid.Path {
		t.Fatalf("cuts=%+v want 仅 mid", cuts)
	}
}

func TestSelectExportCuts_SortedInput(t *testing.T) {
	a := seg("a", base(), base().Add(time.Hour))
	b := seg("b", base().Add(2*time.Hour), base().Add(3*time.Hour))
	// 乱序传入
	cuts := selectExportCuts([]models.RecordingSegment{b, a}, base(), base().Add(4*time.Hour))
	if len(cuts) != 2 || cuts[0].Path != a.Path || cuts[1].Path != b.Path {
		t.Fatalf("cuts=%+v", cuts)
	}
}
