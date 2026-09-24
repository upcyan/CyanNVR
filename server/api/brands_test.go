package api

import (
	"strings"
	"testing"
)

// TestBuildBrandRTSP_HikvisionChannels 海康通道号必须正确映射到 101/102、201/202。
// 这是接 NVR 通道最容易出错的地方（通道 2 不是 102 而是 201）。
func TestBuildBrandRTSP_HikvisionChannels(t *testing.T) {
	cases := []struct {
		ch     int
		wantIn string
	}{
		{1, "/Streaming/Channels/101"},
		{2, "/Streaming/Channels/201"},
		{3, "/Streaming/Channels/301"},
		{0, "/Streaming/Channels/101"}, // 未填默认通道 1
	}
	for _, c := range cases {
		got := buildBrandRTSP(rtspBuildReq{
			Brand: "hik", IP: "192.168.1.64", Username: "admin", Password: "pw", Channel: c.ch,
		})
		main, _ := got["main"].(string)
		if !strings.Contains(main, c.wantIn) {
			t.Errorf("通道 %d: 主码流 %q 应包含 %q", c.ch, main, c.wantIn)
		}
		if strings.Contains(main, "{ch}") {
			t.Errorf("通道 %d: 模板占位符未被替换: %q", c.ch, main)
		}
	}
}

// TestBuildBrandRTSP_DahuaQuery 大华地址含 query，必须正确拼进 URL 而不是路径里。
func TestBuildBrandRTSP_DahuaQuery(t *testing.T) {
	got := buildBrandRTSP(rtspBuildReq{
		Brand: "dahua", IP: "10.0.0.9", Username: "admin", Password: "pw", Channel: 2,
	})
	main, _ := got["main"].(string)
	sub, _ := got["sub"].(string)
	if !strings.Contains(main, "channel=2") || !strings.Contains(main, "subtype=0") {
		t.Errorf("大华主码流 query 不正确: %q", main)
	}
	if !strings.Contains(sub, "subtype=1") {
		t.Errorf("大华子码流 query 不正确: %q", sub)
	}
	// query 不应出现在路径段里
	if strings.Contains(main, "/cam/realmonitor?channel=2&subtype=0?") {
		t.Errorf("query 拼接异常: %q", main)
	}
}

// TestBuildBrandRTSP_CredentialsAndPort 凭证与端口要正确写入。
func TestBuildBrandRTSP_CredentialsAndPort(t *testing.T) {
	got := buildBrandRTSP(rtspBuildReq{
		Brand: "tplink", IP: "192.168.68.54", Port: 8554, Username: "admin", Password: "p@ss",
	})
	main, _ := got["main"].(string)
	if !strings.HasPrefix(main, "rtsp://admin:") {
		t.Errorf("应含用户名: %q", main)
	}
	if !strings.Contains(main, ":8554") {
		t.Errorf("应含自定义端口 8554: %q", main)
	}
	if !strings.Contains(main, "/stream1") {
		t.Errorf("TP-LINK 主码流路径应为 /stream1: %q", main)
	}
	// 特殊字符密码必须被转义（否则 URL 解析会错）
	if strings.Contains(main, "p@ss@") {
		t.Errorf("密码中的 @ 未被转义: %q", main)
	}
}

// TestBuildBrandRTSP_AutoAndCustomReturnEmpty auto/custom 不由后端拼地址。
func TestBuildBrandRTSP_AutoAndCustomReturnEmpty(t *testing.T) {
	for _, b := range []string{"auto", "custom", "nonexistent"} {
		got := buildBrandRTSP(rtspBuildReq{Brand: b, IP: "1.2.3.4"})
		if main, _ := got["main"].(string); main != "" {
			t.Errorf("品牌 %q 不应生成地址，实际 %q", b, main)
		}
	}
}

// TestBrandTemplatesWellFormed 模板表自身要合法：
// id 非空且唯一、显示名非空、需要通道号的必须给出提示。
func TestBrandTemplatesWellFormed(t *testing.T) {
	seen := map[string]bool{}
	for _, b := range brandTemplates {
		if b.ID == "" {
			t.Error("存在空 id 的模板")
		}
		if seen[b.ID] {
			t.Errorf("id 重复: %q", b.ID)
		}
		seen[b.ID] = true
		if b.Name == "" {
			t.Errorf("%s 缺少显示名", b.ID)
		}
		if b.NeedCh && b.ChHint == "" {
			t.Errorf("%s 需要通道号但未给提示", b.ID)
		}
		if b.MainPath != "" && !strings.HasPrefix(b.MainPath, "/") {
			t.Errorf("%s 的主码流路径应以 / 开头: %q", b.ID, b.MainPath)
		}
	}
	// 必须包含 auto 与 custom 两个特殊项
	for _, must := range []string{"auto", "custom"} {
		if !seen[must] {
			t.Errorf("缺少必备模板 %q", must)
		}
	}
}
