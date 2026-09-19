package onvifx

import (
	"bufio"
	"context"
	"encoding/base64"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// XiaomiRTSPSPort 是小米/米家摄像头的 RTSP 默认端口。
// 注意：它不是标准的 554，而是 8554 —— 这也是识别小米设备的关键特征之一。
const XiaomiRTSPSPort = 8554

// xiaomiPath 描述一个小米摄像头的候选 RTSP 路径及其对应型号。
type xiaomiPath struct {
	Path  string
	Model string
}

// xiaomiPaths 汇总社区已知的小米/米家摄像头 RTSP 路径。
//
// 小米摄像头多数为云优先设计，需在米家 App 中开启「局域网监控」或
// 刷入支持 RTSP 的固件后才会开放 RTSP 服务；开启后路径因型号而异，
// 因此这里按常见程度排序逐一尝试。
var xiaomiPaths = []xiaomiPath{
	{"/live/ch00_0", "小米 SXJ01ZM"},
	{"/ch0_0.h264", "小方/小蚁"},
	{"/ch0.h264", "小米通用"},
	{"/unicast", "大方 Dafang/T20"},
	{"/mainstream", "MJSXJ02CM/05CM"},
	{"/substream", "MJSXJ05CM"},
	{"/11", "MJSXJ02HL"},
}

// DiscoverXiaomi 扫描局域网内的小米摄像头。
//
// ONVIF 的 WS-Discovery 只能发现声明支持 ONVIF 的设备，而大量小米
// 摄像头不响应 ONVIF 广播（或其 ONVIF 默认关闭），因此这里改用
// 「端口特征 + 路径探测」的方式主动扫描 RTSP 8554 端口。
//
// 扫描范围取自本机各网卡的 IPv4 子网，仅做 TCP 连接与一次 RTSP 探测，
// 不会向设备发送认证凭据。
func DiscoverXiaomi(ctx context.Context) []Found {
	subnets := localIPv4Subnets()
	if len(subnets) == 0 {
		return nil
	}
	// 排除本机地址：NAS 自身也可能监听 8554（例如同主机上的其它媒体服务），
	// 若不排除，探测自己会得到一个「伪摄像头」，误导用户去添加。
	self := localIPv4Addrs()

	var (
		mu   sync.Mutex
		out  []Found
		seen = map[string]bool{}
		wg   sync.WaitGroup
	)
	sem := make(chan struct{}, 64) // 限制并发，避免冲击局域网

	for _, sn := range subnets {
		for _, ip := range hostsIn(sn) {
			if self[ip] {
				continue
			}
			select {
			case <-ctx.Done():
				wg.Wait()
				return out
			default:
			}
			wg.Add(1)
			sem <- struct{}{}
			go func(ip string) {
				defer wg.Done()
				defer func() { <-sem }()

				// 1) 先用 TCP 快速判断 8554 是否开放
				if !tcpOpen(ctx, ip, XiaomiRTSPSPort, 400*time.Millisecond) {
					return
				}
				// 2) 确认是 RTSP 服务（OPTIONS 能拿到 RTSP/1.0 响应即可，
				//    401 也说明服务存在——小米需要认证）
				if !isRTSPServer(ctx, ip, XiaomiRTSPSPort, "", "", 900*time.Millisecond) {
					return
				}
				// 3) 尝试常见路径，命中即可给出建议 URL
				path, model := firstWorkingXiaomiPath(ctx, ip, XiaomiRTSPSPort)

				mu.Lock()
				defer mu.Unlock()
				key := net.JoinHostPort(ip, strconv.Itoa(XiaomiRTSPSPort))
				if seen[key] {
					return
				}
				seen[key] = true

				name := "小米摄像头"
				if model != "" {
					name = "小米摄像头 (" + model + ")"
				}
				f := Found{
					IP:     ip,
					Port:   XiaomiRTSPSPort,
					Name:   name,
					Vendor: "xiaomi",
				}
				if path != "" {
					f.RTSPURL = fmt.Sprintf("rtsp://%s:%d%s", ip, XiaomiRTSPSPort, path)
				} else {
					f.RTSPURL = fmt.Sprintf("rtsp://%s:%d/live/ch00_0", ip, XiaomiRTSPSPort)
				}
				out = append(out, f)
			}(ip)
		}
	}
	wg.Wait()
	return out
}

// firstWorkingXiaomiPath 依次尝试小米常见路径，返回首个可用的路径与型号。
func firstWorkingXiaomiPath(ctx context.Context, ip string, port int) (string, string) {
	for _, p := range xiaomiPaths {
		if ctx.Err() != nil {
			return "", ""
		}
		// 未认证时报 401 属于正常现象（说明路径存在且需要凭据），
		// 因此这里只要服务器给出了 RTSP 响应即视为该路径有效。
		if rtspPathExists(ctx, ip, port, p.Path, 900*time.Millisecond) {
			return p.Path, p.Model
		}
	}
	return "", ""
}

// tcpOpen 判断 TCP 端口是否可连接。
func tcpOpen(ctx context.Context, ip string, port int, timeout time.Duration) bool {
	d := net.Dialer{Timeout: timeout}
	conn, err := d.DialContext(ctx, "tcp", net.JoinHostPort(ip, strconv.Itoa(port)))
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// isRTSPServer 发送 RTSP OPTIONS，确认目标端口确实是 RTSP 服务。
func isRTSPServer(ctx context.Context, ip string, port int, user, pass string, timeout time.Duration) bool {
	status, err := rtspRequest(ctx, ip, port, "OPTIONS", "*", user, pass, timeout)
	if err != nil {
		return false
	}
	// 200 = 无需认证；401 = 需要认证，两者都说明是 RTSP 服务
	return strings.Contains(status, "200") || strings.Contains(status, "401")
}

// rtspPathExists 用 DESCRIBE 判断某路径是否存在。
// 返回 true 表示服务器对该路径给出了正常应答或要求认证（即路径存在）。
func rtspPathExists(ctx context.Context, ip string, port int, path string, timeout time.Duration) bool {
	status, err := rtspRequest(ctx, ip, port, "DESCRIBE", "rtsp://"+net.JoinHostPort(ip, strconv.Itoa(port))+path, "", "", timeout)
	if err != nil {
		return false
	}
	if strings.Contains(status, "404") || strings.Contains(status, "400") {
		return false
	}
	return strings.Contains(status, "200") || strings.Contains(status, "401") || strings.Contains(status, "403")
}

// rtspRequest 发起一次极简 RTSP 请求并返回状态行。
func rtspRequest(ctx context.Context, ip string, port int, method, target, user, pass string, timeout time.Duration) (string, error) {
	d := net.Dialer{Timeout: timeout}
	conn, err := d.DialContext(ctx, "tcp", net.JoinHostPort(ip, strconv.Itoa(port)))
	if err != nil {
		return "", err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(timeout))

	var sb strings.Builder
	fmt.Fprintf(&sb, "%s %s RTSP/1.0\r\n", method, target)
	fmt.Fprintf(&sb, "CSeq: 1\r\n")
	fmt.Fprintf(&sb, "User-Agent: SimpleNVR\r\n")
	if user != "" {
		fmt.Fprintf(&sb, "Authorization: Basic %s\r\n", basicAuth(user, pass))
	}
	sb.WriteString("\r\n")

	if _, err := conn.Write([]byte(sb.String())); err != nil {
		return "", err
	}
	line, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil {
		return "", err
	}
	return line, nil
}

// basicAuth 生成 HTTP Basic 认证串。
func basicAuth(user, pass string) string {
	return base64.StdEncoding.EncodeToString([]byte(user + ":" + pass))
}

// localIPv4Addrs 返回本机所有 IPv4 地址，用于扫描时排除自己。
// 与 localIPv4Subnets 不同，这里不做物理/虚拟过滤 ——
// 只要是自己持有的地址，无论挂在哪个接口上都必须排除。
func localIPv4Addrs() map[string]bool {
	out := map[string]bool{}
	ifaces, err := net.Interfaces()
	if err != nil {
		return out
	}
	for _, i := range ifaces {
		addrs, err := i.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			if ipnet, ok := a.(*net.IPNet); ok && ipnet.IP.To4() != nil {
				out[ipnet.IP.String()] = true
			}
		}
	}
	return out
}

// virtualIfacePrefixes 是已知虚拟网卡的前缀。
// 这些接口（Docker 网桥、WAF、VPN 隧道等）上不可能存在摄像头，
// 对它们做全网段扫描纯属浪费，还会给同主机的其它容器带来无谓流量。
var virtualIfacePrefixes = []string{
	"docker", "br-", "veth", "virbr", "vmnet", "vboxnet",
	"tun", "tap", "wg", "zt", "tailscale", "safeline",
	"kube", "cni", "flannel", "cali", "lo",
}

// isVirtualIface 按名称前缀判断是否为虚拟网卡。
func isVirtualIface(name string) bool {
	for _, p := range virtualIfacePrefixes {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}

// hasDeviceLink 用内核暴露的 /sys/class/net/<name>/device 链接判断物理网卡。
// 物理网卡（PCIe/USB/virtio）都有该链接，纯软件的虚拟接口没有。
func hasDeviceLink(name string) bool {
	_, err := os.Stat(filepath.Join("/sys/class/net", name, "device"))
	return err == nil
}

// localIPv4Subnets 返回本机可用于摄像头扫描的 IPv4 子网。
//
// 过滤策略（两级）：
//  1. 先按名称前缀排除 docker0 / br-* / veth* / safeline-* 等虚拟接口；
//  2. 再用 /sys/class/net/<name>/device 链接确认是物理网卡。
//
// 若第二级把网卡全过滤掉了（例如极端虚拟化环境），则回退为仅用第一级的结果，
// 避免在特殊环境下扫不到任何网段。
func localIPv4Subnets() []*net.IPNet {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	var physical, fallback []*net.IPNet
	for _, i := range ifaces {
		if i.Flags&net.FlagUp == 0 || i.Flags&net.FlagLoopback != 0 {
			continue
		}
		if isVirtualIface(i.Name) {
			continue
		}
		addrs, err := i.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			if ipnet, ok := a.(*net.IPNet); ok && ipnet.IP.To4() != nil {
				fallback = append(fallback, ipnet)
				if hasDeviceLink(i.Name) {
					physical = append(physical, ipnet)
				}
			}
		}
	}
	if len(physical) > 0 {
		return physical
	}
	return fallback
}

// hostsIn 枚举子网内的可用主机地址（跳过网络号与广播地址）。
// 对大于 /24 的子网仅取前 256 个地址，避免扫描耗时失控。
func hostsIn(n *net.IPNet) []string {
	ip := n.IP.To4()
	if ip == nil {
		return nil
	}
	mask := n.Mask
	ones, bits := mask.Size()
	if bits != 32 {
		return nil
	}
	hostBits := bits - ones
	if hostBits < 2 {
		return nil
	}
	if hostBits > 8 {
		hostBits = 8 // 限制为最多 256 个地址
	}
	base := make(net.IP, len(ip))
	copy(base, ip)
	// 归零主机位：网络位保留，主机位清零。
	// 字节 i 覆盖从左数第 8*i .. 8*i+7 位，因此判断依据是 8*i 而不是 8*(3-i)。
	// （此前按 8*(3-i) 判断，导致 /24 时把首字节 192 清成 0、却保留末字节，
	//   扫描的是 0.168.68.0/24 这种不存在的网段，小米设备永远发现不了。）
	for i := 0; i < 4; i++ {
		start := 8 * i
		switch {
		case start >= ones:
			base[i] = 0 // 整个字节都属于主机位
		case start+8 <= ones:
			// 整个字节都属于网络位，保持不变
		default:
			keep := ones - start // 该字节中属于网络位的高位数
			base[i] &= byte(0xFF << (8 - keep))
		}
	}
	count := 1 << hostBits
	var out []string
	for i := 1; i < count-1; i++ {
		v := int(base[0])<<24 | int(base[1])<<16 | int(base[2])<<8 | int(base[3])
		v += i
		out = append(out, fmt.Sprintf("%d.%d.%d.%d", byte(v>>24), byte(v>>16), byte(v>>8), byte(v)))
	}
	return out
}
