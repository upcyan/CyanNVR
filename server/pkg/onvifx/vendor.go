package onvifx

import (
	"net"
	"os"
	"strconv"
	"strings"
	"time"
)

// VendorUnknown 表示无法判定厂商。
const VendorUnknown = "unknown"

// vendorNames 是厂商标识到展示名的映射。
// 前端按 ID 选择内置 logo 与品牌配色，展示名用于文字标注。
var vendorNames = map[string]string{
	"hikvision":   "海康威视",
	"dahua":       "大华",
	"huawei":      "华为",
	"xiaomi":      "小米",
	"tplink":      "TP-LINK",
	"uniview":     "宇视",
	"tiandy":      "天地伟业",
	"ezviz":       "萤石",
	"vivotek":     "晶睿",
	"axis":        "Axis",
	"bosch":       "博世",
	"hanwha":      "韩华",
	"reolink":     "Reolink",
	"oem":         "通用模组",
	VendorUnknown: "未知厂商",
}

// VendorName 返回厂商标识对应的展示名；未知标识原样返回。
func VendorName(id string) string {
	if n, ok := vendorNames[id]; ok {
		return n
	}
	if id == "" {
		return vendorNames[VendorUnknown]
	}
	return id
}

// vendorKeywords 用于从 ONVIF scopes、设备名等文本里匹配厂商。
// 值均为小写特征串；tp-ipc 是实测到的常见出厂设备名（TP-LINK 的 IPC 系列）。
var vendorKeywords = map[string][]string{
	"hikvision": {"hikvision", "hikvison", "海康"},
	"dahua":     {"dahua", "大华", "imou", "乐橙"},
	"huawei":    {"huawei", "holosens", "好望"},
	"xiaomi":    {"xiaomi", "小米", "mijia", "米家", "chuangmi"},
	"tplink":    {"tp-ipc", "tp-link", "tplink", "tapo"},
	"uniview":   {"uniview", "unv "},
	"tiandy":    {"tiandy"},
	"ezviz":     {"ezviz", "萤石"},
	"vivotek":   {"vivotek"},
	"axis":      {"axis"},
	"bosch":     {"bosch"},
	"hanwha":    {"hanwha", "samsung techwin", "wisenet"},
	"reolink":   {"reolink"},
}

// vendorMatchOrder 固定关键词匹配顺序，避免 map 迭代顺序随机导致
// 同一段文本在不同次运行中得到不同厂商。
var vendorMatchOrder = []string{
	"hikvision", "dahua", "huawei", "xiaomi", "tplink", "uniview",
	"tiandy", "ezviz", "vivotek", "axis", "bosch", "hanwha", "reolink",
}

// VendorFromText 从若干文本中匹配厂商标识，匹配不到返回空串。
func VendorFromText(texts ...string) string {
	for _, t := range texts {
		low := strings.ToLower(strings.TrimSpace(t))
		if low == "" {
			continue
		}
		for _, vid := range vendorMatchOrder {
			for _, kw := range vendorKeywords[vid] {
				if strings.Contains(low, kw) {
					return vid
				}
			}
		}
	}
	return ""
}

// NormalizeOUI 取 MAC 的前 3 字节并转成 6 位大写十六进制。
// 兼容 60:a3:e3:... / 60-A3-E3-... / 60a3e3 等写法；长度不足返回空串。
func NormalizeOUI(mac string) string {
	out := make([]byte, 0, 6)
	for i := 0; i < len(mac) && len(out) < 6; i++ {
		c := mac[i]
		switch {
		case c >= '0' && c <= '9':
			out = append(out, c)
		case c >= 'a' && c <= 'f':
			out = append(out, c-('a'-'A'))
		case c >= 'A' && c <= 'F':
			out = append(out, c)
		}
	}
	if len(out) != 6 {
		return ""
	}
	return string(out)
}

// VendorFromMAC 用 MAC 的 OUI 查 IEEE 厂商表。
func VendorFromMAC(mac string) (string, bool) {
	oui := NormalizeOUI(mac)
	if oui == "" {
		return "", false
	}
	v, ok := ouiVendor[oui]
	return v, ok
}

// readARPTable 读取 /proc/net/arp，返回 IP -> MAC。
//
// 容器以 host 网络模式运行，与主机共享网络命名空间，而 /proc/net/arp
// 是按网络命名空间隔离的，因此这里拿到的正是主机的 ARP 表。
func readARPTable() map[string]string {
	out := map[string]string{}
	b, err := os.ReadFile("/proc/net/arp")
	if err != nil {
		return out
	}
	for i, line := range strings.Split(string(b), "\n") {
		if i == 0 { // 跳过表头
			continue
		}
		f := strings.Fields(line)
		if len(f) < 4 {
			continue
		}
		ip, mac := f[0], f[3]
		if mac == "" || mac == "00:00:00:00:00:00" {
			continue
		}
		out[ip] = mac
	}
	return out
}

// warmARP 主动建立一次 TCP 连接，让内核为对端写入 ARP 表项。
//
// WS-Discovery 走组播，不会产生单播 ARP 记录，因此必须主动发起连接，
// 否则紧接着读 /proc/net/arp 会查不到该设备。连接是否成功并不重要 ——
// 只要发出了 SYN，内核就会先解析 MAC 地址。
func warmARP(host string, port int) {
	if port <= 0 {
		port = 80
	}
	c, err := net.DialTimeout("tcp", net.JoinHostPort(host, strconv.Itoa(port)), 800*time.Millisecond)
	if err == nil {
		_ = c.Close()
	}
}

// sameSubnet 判断 IP 是否属于给定子网之一。
// 跨网段设备在 ARP 表里留下的是网关 MAC，用它查 OUI 会得到路由器厂商，
// 因此跨网段设备必须跳过 OUI 识别。
func sameSubnet(ip string, subnets []*net.IPNet) bool {
	p := net.ParseIP(ip)
	if p == nil {
		return false
	}
	for _, sn := range subnets {
		if sn.Contains(p) {
			return true
		}
	}
	return false
}
