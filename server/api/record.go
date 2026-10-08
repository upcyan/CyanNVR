package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// 全局暂停/恢复录制（管理员操作）
//
// 语义：暂停 = 停止监控。不只是停录像，直播也一起停——否则摄像头仍在被
// 持续访问，隐私诉求（"我要停一会儿"）不成立。前端首页卡片因此显示离线。
//
// 状态持久化到 settings.json：暂停监控属于隐私/维护操作，
// 进程重启后静默恢复录制会违背用户意图。

// recordPauseStatus 返回当前暂停状态（所有登录用户可读，用于渲染按钮）。
func (s *Server) recordPauseStatus(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"paused": s.rec.IsPaused(),
	})
}

// setRecordPause 暂停或恢复录制。body: {paused: bool}
func (s *Server) setRecordPause(c *gin.Context) {
	var body struct {
		Paused bool `json:"paused"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "bad json"})
		return
	}
	// 幂等：重复请求同状态直接返回，避免重复停/拉起造成抖动
	if s.rec.IsPaused() == body.Paused {
		c.JSON(http.StatusOK, gin.H{"paused": body.Paused, "changed": false})
		return
	}

	s.rec.SetPaused(body.Paused)

	// 持久化：暂停状态要跨进程重启保留
	s.settingsMu.Lock()
	s.settings.RecordPaused = body.Paused
	s.settingsMu.Unlock()
	s.saveSettings()

	c.JSON(http.StatusOK, gin.H{"paused": body.Paused, "changed": true})
}
