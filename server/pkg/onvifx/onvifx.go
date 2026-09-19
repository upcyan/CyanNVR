package onvifx

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/beevik/etree"
	onvif "github.com/use-go/onvif"
	"github.com/use-go/onvif/media"
	wsdiscovery "github.com/use-go/onvif/ws-discovery"
	xsdonvif "github.com/use-go/onvif/xsd/onvif"

	"cyannvr/server/models"
)

type Found struct {
	XAddr string `json:"xaddr"`
	IP    string `json:"ip"`
	Port  int    `json:"port"`
	Name  string `json:"name"`
	// Vendor 是归一化厂商标识（hikvision/dahua/xiaomi/tplink/unknown 等），
	// 前端据此选择内置 logo 与品牌配色。
	Vendor string `json:"vendor,omitempty"`
	// Manufacturer 是厂商展示名，例如 "TP-LINK"。
	Manufacturer string `json:"manufacturer,omitempty"`
	// VendorSource 记录厂商判定依据：onvif（设备自报）/ oui（MAC 查表）
	// / xiaomi（端口特征）/ none（未能判定）。
	VendorSource string `json:"vendorSource,omitempty"`
	// Hardware 是设备型号，来自 ONVIF scope hardware 或 GetDeviceInformation。
	Hardware string `json:"hardware,omitempty"`
	// Location 来自 ONVIF scope，例如 ShenZhen。
	Location string `json:"location,omitempty"`
	// MAC 是设备网卡地址，仅在同网段时才会填充。
	MAC string `json:"mac,omitempty"`
	// RTSPURL 是探测到的可用拉流地址（目前用于小米摄像头）。
	RTSPURL string `json:"rtspUrl,omitempty"`
	// Scopes 保留原始 ONVIF scopes，便于排查厂商识别问题。
	Scopes []string `json:"scopes,omitempty"`
}

// Discover 通过 WS-Discovery 探测局域网内的 ONVIF 设备。
//
// 这里直接调用 wsdiscovery 而不走 onvif.GetAvailableDevices 封装，是因为
// 封装层只从 XAddrs 里取地址、把 Scopes 整段丢弃了 —— 而厂商、型号、
// 位置信息恰恰全部在 Scopes 里（实测设备回报 name/TP-IPC 等）。
func Discover(ctx context.Context, iface string) ([]Found, error) {
	names, err := ifaceNames(iface)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var out []Found
	for _, name := range names {
		if ctx.Err() != nil {
			return out, ctx.Err()
		}
		matches, err := wsdiscovery.SendProbe(name, nil,
			[]string{"dn:" + onvif.NVT.String()},
			map[string]string{"dn": "http://www.onvif.org/ver10/network/wsdl"})
		if err != nil {
			continue
		}
		for _, raw := range matches {
			f, ok := parseProbeMatch(raw)
			if !ok || seen[f.IP] {
				continue
			}
			seen[f.IP] = true
			out = append(out, f)
		}
	}
	enrichVendors(out)
	return out, nil
}

// parseProbeMatch 从一条 WS-Discovery ProbeMatch 响应里提取设备信息。
func parseProbeMatch(raw string) (Found, bool) {
	doc := etree.NewDocument()
	if err := doc.ReadFromString(raw); err != nil {
		return Found{}, false
	}
	root := doc.Root()
	if root == nil {
		return Found{}, false
	}
	pm := root.FindElement("./Body/ProbeMatches/ProbeMatch")
	if pm == nil {
		return Found{}, false
	}

	var xaddr string
	if e := pm.FindElement("./XAddrs"); e != nil {
		if fields := strings.Fields(e.Text()); len(fields) > 0 {
			xaddr = fields[0]
		}
	}
	if xaddr == "" {
		return Found{}, false
	}
	host, port := parseHostPort(xaddr)
	if host == "" {
		return Found{}, false
	}

	var scopes []string
	if e := pm.FindElement("./Scopes"); e != nil {
		scopes = strings.Fields(e.Text())
	}

	f := Found{XAddr: xaddr, IP: host, Port: port, Scopes: scopes}
	f.Name = scopeValue(scopes, "name")
	f.Hardware = scopeValue(scopes, "hardware")
	f.Location = scopeValue(scopes, "location")
	// hardware/MODEL 是 OEM 模组未修改的出厂默认值，当成有效型号会误导用户。
	if strings.EqualFold(f.Hardware, "MODEL") {
		f.Hardware = ""
	}
	if f.Name == "" {
		f.Name = host
	}
	return f, true
}

// scopeValue 从 ONVIF scopes 里取出指定类别的值。
// scope 形如 onvif://www.onvif.org/name/TP-IPC，取值部分并做 URL 解码
// （设备名里可能含 %20 之类的转义）。
func scopeValue(scopes []string, kind string) string {
	marker := "/" + strings.ToLower(kind) + "/"
	for _, s := range scopes {
		i := strings.Index(strings.ToLower(s), marker)
		if i < 0 {
			continue
		}
		v := strings.TrimSpace(s[i+len(marker):])
		if dec, err := url.PathUnescape(v); err == nil {
			v = dec
		}
		if v != "" {
			return v
		}
	}
	return ""
}

// enrichVendors 为发现结果补全厂商信息。
//
// 判定顺序：
//  1. 设备自报的特征（ONVIF scopes 与设备名）—— 优先采信，因为这是固件里的品牌；
//  2. MAC 的 OUI —— 用于穿透那些把 scopes 留成出厂默认值的贴牌设备，
//     这类设备往往所有型号都上报同一个名字，只看 scopes 无法区分品牌。
//
// 跨网段设备跳过 OUI：它们在 ARP 表里对应的是网关 MAC，查出来会变成路由器厂商。
func enrichVendors(found []Found) {
	subnets := localIPv4Subnets()

	// 先对同网段设备各建立一次 TCP 连接，让内核写入 ARP 表项。
	// WS-Discovery 走组播不会产生单播 ARP 记录，不预热就查不到 MAC。
	var needARP bool
	for i := range found {
		if sameSubnet(found[i].IP, subnets) {
			warmARP(found[i].IP, found[i].Port)
			needARP = true
		}
	}
	arp := map[string]string{}
	if needARP {
		arp = readARPTable()
		// ARP 解析是异步的，首次建连后条目可能尚未落表（Flags 仍为 0x0），
		// 若有关键条目缺失则稍等片刻补读一次。
		if arpMissing(arp, found, subnets) {
			time.Sleep(200 * time.Millisecond)
			for k, v := range readARPTable() {
				arp[k] = v
			}
		}
	}

	for i := range found {
		f := &found[i]

		// MAC 始终尝试填充：它本身就是有用的展示信息，
		// 也能让用户交叉验证厂商判定结果。
		if mac := arp[f.IP]; mac != "" && sameSubnet(f.IP, subnets) {
			f.MAC = mac
		}

		texts := append([]string{f.Name, f.Hardware}, f.Scopes...)
		switch v := VendorFromText(texts...); {
		case v != "":
			f.Vendor, f.VendorSource = v, "onvif"
		case f.MAC != "":
			if ov, ok := VendorFromMAC(f.MAC); ok {
				f.Vendor, f.VendorSource = ov, "oui"
			}
		}
		if f.Vendor == "" {
			f.Vendor, f.VendorSource = VendorUnknown, "none"
		}
		f.Manufacturer = VendorName(f.Vendor)
	}
}

// arpMissing 报告是否存在「同网段但 ARP 表中还没有 MAC」的设备。
func arpMissing(arp map[string]string, found []Found, subnets []*net.IPNet) bool {
	for _, f := range found {
		if sameSubnet(f.IP, subnets) && arp[f.IP] == "" {
			return true
		}
	}
	return false
}

// ifaceNames resolves the target interface(s) for discovery. When name is
// empty, all non-loopback interfaces with an IPv4 address are used so that
// WS-Discovery broadcasts go out on every active LAN adapter.
func ifaceNames(name string) ([]string, error) {
	if name != "" {
		if _, err := net.InterfaceByName(name); err != nil {
			return nil, err
		}
		return []string{name}, nil
	}
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	var out []string
	for _, i := range ifaces {
		if i.Flags&net.FlagUp == 0 || i.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := i.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			if ipnet, ok := a.(*net.IPNet); ok && ipnet.IP.To4() != nil {
				out = append(out, i.Name)
				break
			}
		}
	}
	return out, nil
}

// GetStreamURI fetches the RTSP URL via ONVIF Media service (best effort).
func GetStreamURI(host string, port int, user, pass string) string {
	streams := GetStreams(host, port, user, pass)
	if len(streams) > 0 {
		return streams[0].URL
	}
	return ""
}

// GetStreams enumerates every ONVIF profile and its RTSP stream URI.
func GetStreams(host string, port int, user, pass string) []models.Stream {
	var out []models.Stream
	if port == 0 {
		port = 80
	}
	dev, err := onvif.NewDevice(onvif.DeviceParams{
		Xaddr:    net.JoinHostPort(host, strconv.Itoa(port)),
		Username: user,
		Password: pass,
	})
	if err != nil {
		// ONVIF 连接失败，尝试常见路径
		return guessStreams(host, user, pass)
	}
	resp, err := dev.CallMethod(media.GetProfiles{})
	if err != nil {
		// ONVIF 获取配置失败，尝试常见路径
		return guessStreams(host, user, pass)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	doc := etree.NewDocument()
	if err := doc.ReadFromString(string(body)); err != nil {
		// XML 解析失败，尝试常见路径
		return guessStreams(host, user, pass)
	}
	var tokens []string
	for _, p := range doc.FindElements("./Envelope/Body/GetProfilesResponse/Profiles/Profile") {
		t := p.SelectAttrValue("token", "")
		if t == "" {
			continue
		}
		tokens = append(tokens, t)
		name := ""
		if n := p.FindElement("./Name"); n != nil {
			name = strings.TrimSpace(n.Text())
		}
		// Try to fetch the URI for this profile now; fall back to guessing.
		uri := streamURI(dev, t)
		if uri == "" {
			uri = guessStreamURL(host, user, pass, t)
		}
		label := name
		if label == "" {
			label = "Profile " + t
		}
		out = append(out, models.Stream{ID: t, Name: label, URL: uri})
	}

	// 如果没有从 ONVIF 获取到流，尝试常见路径
	if len(out) == 0 {
		return guessStreams(host, user, pass)
	}

	return out
}

// guessStreams tries common RTSP paths for both main and sub streams.
func guessStreams(host, user, pass string) []models.Stream {
	var out []models.Stream

	// 尝试主码流
	mainURL := guessStreamURL(host, user, pass, "")
	if mainURL != "" {
		out = append(out, models.Stream{
			ID:   "main",
			Name: "主码流",
			URL:  mainURL,
		})
	}

	// 尝试子码流
	subURL := guessSubStreamURL(host, user, pass, "")
	if subURL != "" {
		out = append(out, models.Stream{
			ID:   "sub",
			Name: "子码流",
			URL:  subURL,
		})
	}

	return out
}

func streamURI(dev *onvif.Device, token string) string {
	resp2, err := dev.CallMethod(media.GetStreamUri{
		StreamSetup: xsdonvif.StreamSetup{
			Stream:    xsdonvif.StreamType("RTP-Unicast"),
			Transport: xsdonvif.Transport{Protocol: xsdonvif.TransportProtocol("RTSP")},
		},
		ProfileToken: xsdonvif.ReferenceToken(token),
	})
	if err != nil {
		return ""
	}
	defer resp2.Body.Close()
	body2, _ := io.ReadAll(resp2.Body)
	doc2 := etree.NewDocument()
	if err := doc2.ReadFromString(string(body2)); err != nil {
		return ""
	}
	for _, u := range doc2.FindElements("./Envelope/Body/GetStreamUriResponse/MediaUri/Uri") {
		uri := strings.TrimSpace(u.Text())
		if strings.HasPrefix(uri, "rtsp://") {
			return uri
		}
	}
	return ""
}

// guessStreamURL builds a likely RTSP URL for brands that don't return a
// usable GetStreamUri response. Used as a fallback only.
func guessStreamURL(host, user, pass, token string) string {
	// 尝试常见的 RTSP 路径，包括主码流和子码流
	paths := []string{
		"/stream1",                             // 主码流 (常见)
		"/stream2",                             // 子码流 (常见)
		"/ch1/main",                            // 主码流 (海康威视)
		"/ch1/sub",                             // 子码流 (海康威视)
		"/cam/realmonitor?channel=1&subtype=0", // 主码流 (大华)
		"/cam/realmonitor?channel=1&subtype=1", // 子码流 (大华)
		"/Streaming/Channels/101",              // 主码流 (华为)
		"/Streaming/Channels/102",              // 子码流 (华为)
	}

	for _, path := range paths {
		u := fmt.Sprintf("rtsp://%s:%d%s", host, 554, path)
		if user != "" {
			u = fmt.Sprintf("rtsp://%s:%s@%s:%d%s", user, pass, host, 554, path)
		}
		return u // 返回第一个路径作为主码流
	}

	// 默认返回 stream1
	u := fmt.Sprintf("rtsp://%s:%d/stream1", host, 554)
	if user != "" {
		u = fmt.Sprintf("rtsp://%s:%s@%s:%d/stream1", user, pass, host, 554)
	}
	return u
}

// guessSubStreamURL attempts to find a sub-stream URL by trying common paths.
func guessSubStreamURL(host, user, pass, token string) string {
	subPaths := []string{
		"/stream2",                             // 子码流 (常见)
		"/ch1/sub",                             // 子码流 (海康威视)
		"/cam/realmonitor?channel=1&subtype=1", // 子码流 (大华)
		"/Streaming/Channels/102",              // 子码流 (华为)
	}

	for _, path := range subPaths {
		u := fmt.Sprintf("rtsp://%s:%d%s", host, 554, path)
		if user != "" {
			u = fmt.Sprintf("rtsp://%s:%s@%s:%d%s", user, pass, host, 554, path)
		}
		return u
	}

	// 如果没有找到子码流，返回空字符串
	return ""
}

func parseHostPort(xaddr string) (string, int) {
	x := xaddr
	if i := strings.Index(x, "://"); i >= 0 {
		x = x[i+3:]
	}
	if i := strings.Index(x, "/"); i >= 0 {
		x = x[:i]
	}
	if x == "" {
		return "", 0
	}
	host, portStr, err := net.SplitHostPort(x)
	if err != nil {
		return x, 80
	}
	port, _ := strconv.Atoi(portStr)
	if port == 0 {
		port = 80
	}
	return host, port
}

func DebugLog(format string, args ...any) {
	log.Printf(format, args...)
}
