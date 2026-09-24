//go:build windows

package api

import (
	"net/http"
	"runtime"
	"time"

	"github.com/gin-gonic/gin"
)

// statusInfo Windows 版：磁盘与进程统计依赖 POSIX 接口，这里只回可移植的部分。
func (s *Server) statusInfo(c *gin.Context) {
	resp := gin.H{
		"version":    CoreVersion,
		"serverTime": time.Now().Format(time.RFC3339),
		"process":    gin.H{"goroutines": runtime.NumGoroutine()},
	}
	if devs, err := s.st.ListDevices(); err == nil {
		online, recording := 0, 0
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
