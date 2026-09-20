package main

import (
	"log"
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

	service, err := mdns.NewMDNSService(hostname, "_cyannvr._tcp", "", "", p, nil, []string{"name=CyanNVR"})
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
