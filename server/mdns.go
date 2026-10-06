package main

import (
	"log"
	"net"
	"os"
	"strconv"

	"github.com/hashicorp/mdns"
)

// startMDNS 在局域网内广播 _cyannvr._tcp 服务，供 CyanNVR App 等客户端自动发现。
// mDNS 依赖二层组播：docker bridge 网络下组播不可达（需 --network=host），
// 失败仅记录日志，不影响服务本体。
func startMDNS(port string) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("mDNS announce panic: %v", r)
		}
	}()

	p, err := strconv.Atoi(port)
	if err != nil || p <= 0 {
		p = 8080
	}

	hostname := "cyannvr"
	if h, err := os.Hostname(); err == nil && h != "" {
		hostname = h
	}

	// 必须显式传本机地址：传 nil 时库内部会按 FQDN（hostname + 尾点）做 DNS 解析，
	// 而 /etc/hosts 只登记了短主机名，解析必然失败——mDNS 自上线起从未广播成功过。
	ips := localAnnounceIPs()
	service, err := mdns.NewMDNSService(hostname, "_cyannvr._tcp", "", "", p, ips, []string{"name=CyanNVR"})
	if err != nil {
		log.Printf("mDNS service init failed: %v", err)
		return
	}
	srv, err := mdns.NewServer(&mdns.Config{Zone: service})
	if err != nil {
		log.Printf("mDNS announce unavailable: %v", err)
		return
	}
	log.Printf("mDNS announcing _cyannvr._tcp on port %d", p)
	// 保持引用防止被 GC 关闭；进程退出时由内核回收
	_ = srv
}

// localAnnounceIPs 收集非环回的本机地址作为 mDNS 广播地址。
func localAnnounceIPs() []net.IP {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil
	}
	var ips []net.IP
	for _, a := range addrs {
		ipnet, ok := a.(*net.IPNet)
		if !ok || ipnet.IP.IsLoopback() || ipnet.IP.IsLinkLocalUnicast() {
			continue
		}
		ips = append(ips, ipnet.IP)
	}
	return ips
}
