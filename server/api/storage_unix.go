//go:build !windows

package api

import (
	"net/http"
	"syscall"

	"github.com/gin-gonic/gin"
)

func (s *Server) storageInfo(c *gin.Context) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(s.cfg.DataDir, &stat); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	total := stat.Blocks * uint64(stat.Bsize)
	free := stat.Bavail * uint64(stat.Bsize)
	used := total - free
	toGB := func(b uint64) float64 {
		return float64(b) / (1024 * 1024 * 1024)
	}
	c.JSON(http.StatusOK, gin.H{
		"totalGB": mathRound(toGB(total)),
		"usedGB":  mathRound(toGB(used)),
	})
}
