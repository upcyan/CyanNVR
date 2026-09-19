package onvifx

import "testing"

// TestVendorFromMACRealDevices 用本次实测到的两台摄像头 MAC 验证 OUI 识别。
// 这两台设备的 ONVIF scopes 自报 name/TP-IPC（OEM 默认值），
// 只有 MAC OUI 才能确定品牌，是 OUI 通道的典型用例。
func TestVendorFromMACRealDevices(t *testing.T) {
	cases := []struct{ mac, want string }{
		{"60:a3:e3:d4:52:43", "tplink"}, // 门外
		{"74:39:89:4d:66:6d", "tplink"}, // 客厅
		{"60-A3-E3-D4-52-43", "tplink"}, // 连字符写法
		{"60a3e3", "tplink"},            // 无分隔符
	}
	for _, c := range cases {
		got, ok := VendorFromMAC(c.mac)
		if !ok || got != c.want {
			t.Errorf("VendorFromMAC(%q) = %q,%v; want %q,true", c.mac, got, ok, c.want)
		}
	}
}

func TestVendorFromMACUnknown(t *testing.T) {
	if _, ok := VendorFromMAC("02:00:00:00:00:01"); ok {
		t.Error("本地管理地址不应命中任何厂商")
	}
	if _, ok := VendorFromMAC(""); ok {
		t.Error("空 MAC 不应命中")
	}
	if _, ok := VendorFromMAC("zz:zz"); ok {
		t.Error("非法 MAC 不应命中")
	}
}

// TestVendorFromTextScopes 验证从 ONVIF scopes / 设备名匹配厂商。
func TestVendorFromTextScopes(t *testing.T) {
	cases := []struct{ name, text, want string }{
		{"海康 scope", "onvif://www.onvif.org/name/HIKVISION DS-2CD", "hikvision"},
		{"大华 scope", "onvif://www.onvif.org/name/Dahua IPC-HDW", "dahua"},
		{"华为 scope", "onvif://www.onvif.org/name/Huawei HoloSens", "huawei"},
		{"小米设备名", "小米摄像头 (MJSXJ)", "xiaomi"},
		{"TP-LINK 出厂名", "onvif://www.onvif.org/name/TP-IPC", "tplink"},
		{"宇视", "onvif://www.onvif.org/name/Uniview IPC", "uniview"},
		{"萤石", "onvif://www.onvif.org/name/EZVIZ CS-C6", "ezviz"},
	}
	for _, c := range cases {
		if got := VendorFromText(c.text); got != c.want {
			t.Errorf("%s: VendorFromText(%q) = %q, want %q", c.name, c.text, got, c.want)
		}
	}
	if got := VendorFromText("onvif://www.onvif.org/name/SomeUnknownCam"); got != "" {
		t.Errorf("未知设备名不应匹配到厂商，得到 %q", got)
	}
}

// TestNormalizeOUI 验证 MAC 归一化的各种写法与边界。
func TestNormalizeOUI(t *testing.T) {
	cases := []struct{ in, want string }{
		{"60:a3:e3:d4:52:43", "60A3E3"},
		{"60-A3-E3-D4-52-43", "60A3E3"},
		{"60a3e3", "60A3E3"},
		{"60A3E3", "60A3E3"},
		{"60", ""},
		{"", ""},
		{"zz:zz:zz", ""},
	}
	for _, c := range cases {
		if got := NormalizeOUI(c.in); got != c.want {
			t.Errorf("NormalizeOUI(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestVendorName 验证展示名映射。
func TestVendorName(t *testing.T) {
	if got := VendorName("tplink"); got != "TP-LINK" {
		t.Errorf("VendorName(tplink) = %q, want TP-LINK", got)
	}
	if got := VendorName("xiaomi"); got != "小米" {
		t.Errorf("VendorName(xiaomi) = %q, want 小米", got)
	}
	if got := VendorName(""); got != "未知厂商" {
		t.Errorf("空标识应回退到未知厂商，得到 %q", got)
	}
}
