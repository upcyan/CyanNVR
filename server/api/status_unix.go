//go:build !windows

package api

import (
	"net/http"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
)

// statusInfo 返回运行状态总览：资源占用、磁盘水位、录像概况。
//
// 与 /api/health（只回版本，供探活与前端版本自检）分工不同：
// 这个接口给「设置页 → 运行状态」用，回答的是「这台 NAS 现在吃多少资源、
// 盘还剩多少、录像管不管得住」。
func (s *Server) statusInfo(c *gin.Context) {
	resp := gin.H{
		"version":    CoreVersion,
		"serverTime": time.Now().Format(time.RFC3339),
		"disk":       s.diskSnapshot(),
		"process":    processStats(),
	}
	if devs, err := s.st.ListDevices(); err == nil {
		online := 0
		recording := 0
		for _, d := range devs {
			if d.Online {
				online++
			}
			if d.RecordEnabled {
				recording++
			}
		}
		resp["devices"] = gin.H{"total": len(devs), "online": online, "recording": recording}
	}
	if s.rec != nil {
		resp["ffmpeg"] = s.rec.ProcStats()
	}
	c.JSON(http.StatusOK, resp)
}

// diskSnapshot 返回录像盘与水位的概况。
func (s *Server) diskSnapshot() gin.H {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(s.cfg.DataDir, &stat); err != nil {
		return gin.H{"error": err.Error()}
	}
	total := stat.Blocks * uint64(stat.Bsize)
	free := stat.Bavail * uint64(stat.Bsize)
	used := total - free
	pct := 0.0
	if total > 0 {
		pct = float64(used) / float64(total) * 100
	}
	return gin.H{
		"totalGB": mathRound(float64(total) / (1 << 30)),
		"usedGB":  mathRound(float64(used) / (1 << 30)),
		"freeGB":  mathRound(float64(free) / (1 << 30)),
		"usedPct": mathRound(pct),
		"path":    s.cfg.DataDir,
		// 供界面提示「低于该水位会暂停录像」
		"minFreeMB": s.cfg.MinFreeMB,
		"low":       s.cfg.MinFreeMB > 0 && free < uint64(s.cfg.MinFreeMB)<<20,
	}
}

// processStats 读取本进程的资源占用。
//
// rss 从 /proc/self/statm 取（常驻内存页数）；cpu 用两次 jiffies 采样的
// 差值估算，调用间隔由调用方决定，因此这里同时返回 cpuReady 表示本次是否
// 已能算出 CPU（首次调用只建立基准）。所有读取失败都静默降级，
// 不因为拿不到统计就让整个状态接口失败。
func processStats() gin.H {
	out := gin.H{"goroutines": goroutineCount()}
	if rss, ok := readRSSBytes(); ok {
		out["memMB"] = mathRound(float64(rss) / (1 << 20))
	}
	if cpu, ok := cpuPercentSinceLastSample(); ok {
		out["cpuPercent"] = mathRound(cpu)
	}
	if up, ok := processUptimeSeconds(); ok {
		out["uptimeSec"] = up
	}
	return out
}

func goroutineCount() int {
	b, err := os.ReadFile("/proc/self/status")
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(line, "Threads:") {
			if n, err := strconv.Atoi(strings.TrimSpace(line[len("Threads:"):])); err == nil {
				return n
			}
		}
	}
	return 0
}

func readRSSBytes() (uint64, bool) {
	b, err := os.ReadFile("/proc/self/statm")
	if err != nil {
		return 0, false
	}
	fields := strings.Fields(string(b))
	if len(fields) < 2 {
		return 0, false
	}
	pages, err := strconv.ParseUint(fields[1], 10, 64)
	if err != nil {
		return 0, false
	}
	return pages * uint64(os.Getpagesize()), true
}

func processUptimeSeconds() (int64, bool) {
	b, err := os.ReadFile("/proc/self/stat")
	if err != nil {
		return 0, false
	}
	// 第 22 个字段是 starttime（单位 jiffies）；用系统 uptime 减去它
	s := string(b)
	i := strings.LastIndexByte(s, ')')
	if i < 0 {
		return 0, false
	}
	fields := strings.Fields(s[i+1:])
	if len(fields) < 20 {
		return 0, false
	}
	startTicks, err := strconv.ParseInt(fields[19], 10, 64)
	if err != nil {
		return 0, false
	}
	up, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return 0, false
	}
	parts := strings.Fields(string(up))
	if len(parts) == 0 {
		return 0, false
	}
	upSec, err := strconv.ParseFloat(parts[0], 64)
	if err != nil {
		return 0, false
	}
	const hz = 100 // Linux 默认 USER_HZ
	return int64(upSec) - startTicks/hz, true
}

// cpuSample 保存上一次的 jiffies 与时刻，用于算两次采样之间的 CPU 占用。
var cpuSample struct {
	ticks uint64
	at    time.Time
	valid bool
}

// cpuPercentSinceLastSample 返回距上次调用之间的平均 CPU 百分比。
// 第一次调用只建立基准，返回 ok=false（避免把开机以来的均值当成当前值）。
func cpuPercentSinceLastSample() (float64, bool) {
	ticks, ok := readSelfJiffies()
	if !ok {
		return 0, false
	}
	now := time.Now()
	prev := cpuSample
	cpuSample = struct {
		ticks uint64
		at    time.Time
		valid bool
	}{ticks, now, true}
	if !prev.valid || now.Sub(prev.at) <= 0 || ticks < prev.ticks {
		return 0, false
	}
	dt := now.Sub(prev.at).Seconds()
	const hz = 100 // jiffies/秒
	pct := float64(ticks-prev.ticks) / hz / dt * 100
	return pct, true
}

func readSelfJiffies() (uint64, bool) {
	b, err := os.ReadFile("/proc/self/stat")
	if err != nil {
		return 0, false
	}
	s := string(b)
	i := strings.LastIndexByte(s, ')')
	if i < 0 {
		return 0, false
	}
	fields := strings.Fields(s[i+1:])
	// utime(12) + stime(13)，去掉 comm 后的索引为 11、12
	if len(fields) < 13 {
		return 0, false
	}
	u, err1 := strconv.ParseUint(fields[11], 10, 64)
	ss, err2 := strconv.ParseUint(fields[12], 10, 64)
	if err1 != nil || err2 != nil {
		return 0, false
	}
	return u + ss, true
}
