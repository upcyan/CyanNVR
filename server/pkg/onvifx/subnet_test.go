package onvifx

import (
	"net"
	"testing"
)

func mustCIDR(t *testing.T, s string) *net.IPNet {
	t.Helper()
	_, n, err := net.ParseCIDR(s)
	if err != nil {
		t.Fatalf("bad cidr %s: %v", s, err)
	}
	return n
}

// TestIsVirtualIface 确保局域网扫描跳过 Docker/WAF/VPN 等虚拟网卡。
// 这是扫描耗时从 1778 个地址降到 254 个地址的关键。
func TestIsVirtualIface(t *testing.T) {
	virtual := []string{
		"docker0", "br-691b8552d29e", "br-83055faa3c15", "veth1a2b3c",
		"safeline-ce", "virbr0", "vmnet8", "vboxnet0",
		"tun0", "tap0", "wg0", "ztxxxx", "tailscale0",
		"cni0", "flannel.1", "cali1234", "lo",
	}
	for _, n := range virtual {
		if !isVirtualIface(n) {
			t.Errorf("%s 应判为虚拟网卡（否则会被扫描，浪费大量时间）", n)
		}
	}

	physical := []string{"enp4s0", "eth0", "eth1", "wlan0", "eno1", "ens18", "enx001122334455"}
	for _, n := range physical {
		if isVirtualIface(n) {
			t.Errorf("%s 是物理网卡，不应被跳过", n)
		}
	}
}

// TestLocalIPv4Addrs 确认本机地址能被枚举出来。
// DiscoverXiaomi 依赖它排除自己：NAS 自身可能监听 8554（同主机的其它媒体服务），
// 不排除就会把自己探测成一个「伪小米摄像头」。
func TestLocalIPv4Addrs(t *testing.T) {
	self := localIPv4Addrs()
	if len(self) == 0 {
		t.Fatal("本机地址集合为空，扫描将无法排除自己")
	}
	if !self["127.0.0.1"] {
		t.Errorf("本机地址集合应包含回环地址 127.0.0.1，实际 %v", self)
	}
}

// TestHostsIn 确保 hostsIn 只枚举可用主机地址且规模可控。
func TestHostsIn(t *testing.T) {
	n := mustCIDR(t, "192.168.68.0/24")
	got := hostsIn(n)
	if len(got) != 254 {
		t.Errorf("/24 应有 254 个可用地址，实际 %d", len(got))
	}
	if len(got) > 0 && (got[0] != "192.168.68.1" || got[len(got)-1] != "192.168.68.254") {
		t.Errorf("地址范围异常: 首=%s 尾=%s", got[0], got[len(got)-1])
	}

	// 大于 /24 的网段必须被截断，否则扫描耗时失控。
	big := mustCIDR(t, "172.17.0.0/16")
	got16 := hostsIn(big)
	if len(got16) > 256 {
		t.Errorf("/16 应被截断到 256 个地址以内，实际 %d", len(got16))
	}
	if len(got16) > 0 && got16[0] != "172.17.0.1" {
		t.Errorf("/16 首地址应为 172.17.0.1，实际 %s（网络位被错误清零）", got16[0])
	}

	// /25 覆盖「字节内部分位属于网络位」的分支
	p25 := mustCIDR(t, "192.168.68.128/25")
	got25 := hostsIn(p25)
	if len(got25) != 126 || got25[0] != "192.168.68.129" {
		t.Errorf("/25 异常: len=%d 首=%v（应为 126 个，首 192.168.68.129）", len(got25), got25[0])
	}

	// /30 覆盖「最后一字节多数位为主机位」的分支
	p30 := mustCIDR(t, "192.168.1.4/30")
	got30 := hostsIn(p30)
	if len(got30) != 2 || got30[0] != "192.168.1.5" {
		t.Errorf("/30 异常: len=%d %v（应为 192.168.1.5/6）", len(got30), got30)
	}

	// 主机位不足 2 位的网段（/31 /32）不应产生地址
	if got := hostsIn(mustCIDR(t, "192.168.1.0/31")); len(got) != 0 {
		t.Errorf("/31 不应产生地址，实际 %v", got)
	}
}
