//go:build windows

package api

import (
	"net/http"
	"os/exec"
	"runtime"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

func (s *Server) storageInfo(c *gin.Context) {
	disk := s.cfg.DataDir
	if len(disk) >= 2 && disk[1] == ':' {
		disk = string(disk[0]) + ":"
	} else {
		disk = "C:"
	}

	var totalGB, usedGB float64

	if runtime.GOOS == "windows" {
		powerShell := "Get-PSDrive " + disk[0:1] + " | Select-Object Used,Free | ConvertTo-Json"
		out, err := exec.Command("powershell", "-Command", powerShell).Output()
		if err == nil {
			s := string(out)
			lines := strings.Split(s, "\n")
			var used, free float64
			for _, line := range lines {
				line = strings.TrimSpace(line)
				if strings.HasPrefix(line, `"Used"`) {
					v := strings.TrimPrefix(line, `"Used"`)
					v = strings.TrimLeft(v, " :")
					v = strings.TrimSpace(v)
					used, _ = strconv.ParseFloat(v, 64)
				}
				if strings.HasPrefix(line, `"Free"`) {
					v := strings.TrimPrefix(line, `"Free"`)
					v = strings.TrimLeft(v, " :")
					v = strings.TrimSpace(v)
					free, _ = strconv.ParseFloat(v, 64)
				}
			}
			totalGB = mathRound((used + free) / (1024 * 1024 * 1024))
			usedGB = mathRound(used / (1024 * 1024 * 1024))
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"totalGB": totalGB,
		"usedGB":  usedGB,
	})
}
