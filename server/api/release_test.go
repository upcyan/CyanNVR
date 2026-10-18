package api

import "testing"

func TestParsePlatformVersionsFromAssets(t *testing.T) {
	// 复刻真实事故：v1.10.35 的 tag 下挂着 1.10.34-6 的 fpk 与 1.10.33 的 apk。
	// 按 tag 比对会认为「1.10.35 可用」，实际两端都没有 1.10.35 的包。
	got := parsePlatformVersions([]releaseAsset{
		{Name: "CyanNVR_1.10.34-6_x86.fpk"},
		{Name: "CyanNVR_1.10.33_android.apk"},
	})
	if got.Fpk != "1.10.34-6" {
		t.Errorf("fpk = %q, want 1.10.34-6", got.Fpk)
	}
	if got.Android != "1.10.33" {
		t.Errorf("android = %q, want 1.10.33", got.Android)
	}
}

func TestParsePicksHighestWhenDuplicated(t *testing.T) {
	// 同平台多份资产时取最高版本，避免字母序误选旧包（曾导致 App 更新死循环）。
	got := parsePlatformVersions([]releaseAsset{
		{Name: "CyanNVR_1.10.30_android.apk"},
		{Name: "CyanNVR_1.10.33_android.apk"},
	})
	if got.Android != "1.10.33" {
		t.Errorf("android = %q, want 1.10.33", got.Android)
	}
}

func TestParseIgnoresUnrelatedAssets(t *testing.T) {
	got := parsePlatformVersions([]releaseAsset{
		{Name: "checksums.txt"},
		{Name: "CyanNVR_1.10.34_x86.fpk.bak"},
	})
	if got.Fpk != "" || got.Android != "" {
		t.Errorf("unexpected parse: %+v", got)
	}
}

func TestCompareReleaseVersion(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"1.10.34-6", "1.10.34-5", 1},
		{"1.10.34-1", "1.10.34-1", 0},
		{"1.10.34-6", "1.10.34", 1}, // 同核心，有修订号更大
		{"1.10.33", "1.10.34", -1},
		{"1.10.34", "1.10.35", -1},
		{"v1.10.34", "1.10.34", 0},
		{"1.10.34-10", "1.10.34-9", 1}, // 修订号按数值比较，非字典序
	}
	for _, c := range cases {
		if got := compareReleaseVersion(c.a, c.b); got != c.want {
			t.Errorf("compare(%q,%q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestNoUpdateWhenPlatformMissing(t *testing.T) {
	// 平台产物缺失时 latest 为空，调用方必须当作「无法判断」而非「有更新」。
	got := parsePlatformVersions([]releaseAsset{{Name: "CyanNVR_1.10.34-6_x86.fpk"}})
	if got.Android != "" {
		t.Fatalf("android should be empty, got %q", got.Android)
	}
}
