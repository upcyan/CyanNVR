package onvifx

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"strconv"
	"strings"

	"github.com/beevik/etree"
	onvif "github.com/use-go/onvif"
	"github.com/use-go/onvif/media"
	xsdonvif "github.com/use-go/onvif/xsd/onvif"

	"simplenvr/server/models"
)

type Found struct {
	XAddr string `json:"xaddr"`
	IP    string `json:"ip"`
	Port  int    `json:"port"`
	Name  string `json:"name"`
	// Vendor 标识来源：onvif（标准发现）或 xiaomi（端口扫描发现）
	Vendor string `json:"vendor,omitempty"`
	// RTSPURL 是探测到的可用拉流地址（目前用于小米摄像头）
	RTSPURL string `json:"rtspUrl,omitempty"`
}

func Discover(ctx context.Context, iface string) ([]Found, error) {
	names, err := ifaceNames(iface)
	if err != nil {
		return nil, err
	}
	seen := map[string]Found{}
	var out []Found
	for _, name := range names {
		if ctx.Err() != nil {
			return out, ctx.Err()
		}
		devs, err := onvif.GetAvailableDevicesAtSpecificEthernetInterface(name)
		if err != nil {
			continue
		}
		for _, d := range devs {
			params := d.GetDeviceParams()
			host, port := parseHostPort(params.Xaddr)
			if host == "" {
				continue
			}
			key := net.JoinHostPort(host, strconv.Itoa(port))
			if _, dup := seen[key]; dup {
				continue
			}
			f := Found{XAddr: params.Xaddr, IP: host, Port: port}
			if info := d.GetDeviceInfo(); info.Model != "" {
				f.Name = info.Model
			} else {
				f.Name = host
			}
			seen[key] = f
			out = append(out, f)
			select {
			case <-ctx.Done():
				return out, ctx.Err()
			default:
			}
		}
	}
	return out, nil
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
		"/stream1",           // 主码流 (常见)
		"/stream2",           // 子码流 (常见)
		"/ch1/main",          // 主码流 (海康威视)
		"/ch1/sub",           // 子码流 (海康威视)
		"/cam/realmonitor?channel=1&subtype=0", // 主码流 (大华)
		"/cam/realmonitor?channel=1&subtype=1", // 子码流 (大华)
		"/Streaming/Channels/101", // 主码流 (华为)
		"/Streaming/Channels/102", // 子码流 (华为)
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
		"/stream2",           // 子码流 (常见)
		"/ch1/sub",           // 子码流 (海康威视)
		"/cam/realmonitor?channel=1&subtype=1", // 子码流 (大华)
		"/Streaming/Channels/102", // 子码流 (华为)
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
