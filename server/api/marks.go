package api

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"cyannvr/server/models"
)

var errBadTime = errors.New("bad time")

// 用户标记（回放时间轴打点）
//
// 设计要点：
//   - 持久化到 marks 表。此前只存前端内存数组，退出页面即丢，
//     用户无法积累「要回头看的时刻」。
//   - 按设备 + 时间范围查询：时间轴跨日滑动时按可视窗口增量取用，
//     不必每次拉全量。
//   - 幂等新增：同一时刻 1 秒内重复点击返回已有标记，避免叠出重合旗标。

// listMarks 返回标记列表。
//
// 查询参数（三选一，按优先级）：
//
//	deviceId + from + to  → 取该时间范围（跨日滑动用，from/to 为 RFC3339 或毫秒）
//	deviceId              → 取该设备全部标记（上限 2000，防一次拉爆）
//	（无 deviceId）        → 取所有设备的标记（同上限）
func (s *Server) listMarks(c *gin.Context) {
	deviceID := c.Query("deviceId")
	fromStr := c.Query("from")
	toStr := c.Query("to")

	var from, to time.Time
	hasRange := false
	if fromStr != "" && toStr != "" {
		f, err1 := parseMarkTime(fromStr)
		t, err2 := parseMarkTime(toStr)
		if err1 != nil || err2 != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "bad from/to (RFC3339 or epoch millis)"})
			return
		}
		if !t.After(f) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "to must be after from"})
			return
		}
		from, to, hasRange = f, t, true
	}

	var (
		marks []models.Mark
		err   error
	)
	switch {
	case hasRange:
		marks, err = s.st.MarksForRange(deviceID, from, to)
	case deviceID != "":
		// 无范围时给一个足够宽的范围（2000 年 ~ 现在+10 年），复用同一查询
		marks, err = s.st.MarksForRange(deviceID,
			time.Date(2000, 1, 1, 0, 0, 0, 0, time.Local),
			time.Now().AddDate(10, 0, 0))
	default:
		marks, err = s.st.MarksForRange("",
			time.Date(2000, 1, 1, 0, 0, 0, 0, time.Local),
			time.Now().AddDate(10, 0, 0))
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	// 上限保护：标记是人工打点，正常量级很小；异常情况下也不让响应无限大
	const maxMarks = 2000
	truncated := false
	if len(marks) > maxMarks {
		marks = marks[:maxMarks]
		truncated = true
	}
	c.JSON(http.StatusOK, gin.H{"marks": marks, "truncated": truncated})
}

// createMark 新增标记。body: {deviceId, time, note?}
func (s *Server) createMark(c *gin.Context) {
	var body struct {
		DeviceID string `json:"deviceId"`
		Time     any    `json:"time"`
		Note     string `json:"note"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "bad json"})
		return
	}
	if body.DeviceID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "deviceId required"})
		return
	}
	// 校验设备存在：否则会留下永远查不到的孤儿标记
	if d, err := s.st.GetDevice(body.DeviceID); err != nil || d == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "device not found"})
		return
	}
	// time 允许 RFC3339 字符串或 epoch 毫秒（前端用毫秒更直接）
	ts, err := parseMarkTimeAny(body.Time)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "bad time (RFC3339 or epoch millis)"})
		return
	}
	m := models.Mark{
		ID:        uuid.NewString(),
		DeviceID:  body.DeviceID,
		Time:      ts,
		Note:      strings.TrimSpace(body.Note),
		CreatedAt: time.Now(),
	}
	saved, err := s.st.CreateMark(m)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"mark": saved})
}

// deleteMark 删除标记（按 id + deviceId，避免跨设备误删）。
func (s *Server) deleteMark(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "id required"})
		return
	}
	deviceID := c.Query("deviceId")
	ok, err := s.st.DeleteMark(deviceID, id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "mark not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// updateMark 修改标记备注。body: {deviceId, note}
func (s *Server) updateMark(c *gin.Context) {
	id := c.Param("id")
	var body struct {
		DeviceID string `json:"deviceId"`
		Note     string `json:"note"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "bad json"})
		return
	}
	ok, err := s.st.UpdateMarkNote(body.DeviceID, id, strings.TrimSpace(body.Note))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "mark not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// parseMarkTime 解析 RFC3339 字符串或 epoch 毫秒字符串。
func parseMarkTime(sv string) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339, sv); err == nil {
		return t, nil
	}
	// epoch 毫秒
	if ms, err := strconv.ParseInt(strings.TrimSpace(sv), 10, 64); err == nil {
		return time.UnixMilli(ms), nil
	}
	return time.Time{}, errBadTime
}

// parseMarkTimeAny 处理 JSON 里可能是字符串也可能是数字的时间字段。
func parseMarkTimeAny(v any) (time.Time, error) {
	switch t := v.(type) {
	case string:
		return parseMarkTime(t)
	case float64: // JSON 数字统一解成 float64
		return time.UnixMilli(int64(t)), nil
	default:
		return time.Time{}, errBadTime
	}
}
