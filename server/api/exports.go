package api

// 录像导出：把选定时间范围内的录像段拼接成一个 mp4 文件。
//
// 设计要点：
//   - 任务登记在内存（进程重启即失效，导出成品文件保留在磁盘直到被删除）。
//     前端徽标展示「进行中的任务数」，重启后自然清零，符合语义。
//   - 导出目录固定为 DataDir/exports：不放在 RecordDir 下，避免被录像清理
//     （pruneEmptyDirs / 保留期回收）误伤——RecordDir 下的非日期目录虽不会被
//     pruneEmptyDirs 删除，但保留期回收按日期目录工作，分开存放更直观。
//   - 拼接用 ffmpeg concat demuxer + -c copy：同设备的分段编码参数一致，
//     无需重编码，几十分钟的段几秒就能拼完；起止点用 inpoint/outpoint
//     微秒裁剪，精度受关键帧限制（拷贝流无法精确到帧），可接受。
//   - 单任务串行执行（sem=1）：拼接是磁盘密集型，并发只会互相拖慢。

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"cyannvr/server/config"
	"cyannvr/server/models"
	"cyannvr/server/store"

	"github.com/gin-gonic/gin"
)

// 单次导出允许的最大跨度：界面按「天」选择，这里放宽到 7 天以防误传。
const exportMaxSpan = 7 * 24 * time.Hour

type ExportTask struct {
	ID         string    `json:"id"`
	DeviceID   string    `json:"deviceId"`
	DeviceName string    `json:"deviceName"`
	Start      time.Time `json:"start"`
	End        time.Time `json:"end"`
	Status     string    `json:"status"` // pending | running | done | error
	Error      string    `json:"error,omitempty"`
	FileName   string    `json:"fileName,omitempty"` // 下载名（ASCII，避免 CJK 头兼容问题）
	Size       int64     `json:"size,omitempty"`

	file string // 导出产物磁盘路径（不进 JSON）
	CreatedAt  time.Time `json:"createdAt"`

	cmd *exec.Cmd // 运行中的 ffmpeg 进程，删除任务时强杀
}

type exportManager struct {
	mu     sync.Mutex
	tasks  map[string]*ExportTask
	order  []string // 创建顺序（列表按创建时间倒序返回）
	dir    string   // 导出产物目录
	ffmpeg string
	st     *store.Store
	sem    chan struct{} // 串行执行
}

func newExportManager(cfg *config.Config, st *store.Store) *exportManager {
	m := &exportManager{
		tasks:  map[string]*ExportTask{},
		dir:    filepath.Join(cfg.DataDir, "exports"),
		ffmpeg: cfg.Ffmpeg,
		st:     st,
		sem:    make(chan struct{}, 1),
	}
	_ = os.MkdirAll(m.dir, 0o755)
	return m
}

// create 校验并登记任务，立即返回；执行在后台串行进行。
func (m *exportManager) create(deviceID string, start, end time.Time) (*ExportTask, error) {
	if deviceID == "" {
		return nil, fmt.Errorf("deviceId required")
	}
	if !start.Before(end) {
		return nil, fmt.Errorf("起始时间必须早于截止时间")
	}
	if end.Sub(start) > exportMaxSpan {
		return nil, fmt.Errorf("单次导出最多 7 天")
	}
	dev, err := m.st.GetDevice(deviceID)
	name := deviceID
	if err == nil && dev != nil && dev.Name != "" {
		name = dev.Name
	}

	buf := make([]byte, 5)
	_, _ = rand.Read(buf)
	id := time.Now().UTC().Format("20060102150405") + "-" + hex.EncodeToString(buf)

	t := &ExportTask{
		ID:         id,
		DeviceID:   deviceID,
		DeviceName: name,
		Start:      start,
		End:        end,
		Status:     "pending",
		FileName:   fmt.Sprintf("cyannvr_%s_%s.mp4", deviceID, start.Format("20060102-150405")),
		CreatedAt:  time.Now(),
	}

	m.mu.Lock()
	m.tasks[t.ID] = t
	m.order = append(m.order, t.ID)
	m.mu.Unlock()

	go m.run(t)
	return t, nil
}

func (m *exportManager) get(id string) *ExportTask {
	m.mu.Lock()
	defer m.mu.Unlock()
	t := m.tasks[id]
	if t != nil {
		cp := *t
		return &cp
	}
	return nil
}

// list 按创建时间倒序返回全部任务的快照。
func (m *exportManager) list() []*ExportTask {
	m.mu.Lock()
	ids := append([]string(nil), m.order...)
	snap := make([]*ExportTask, 0, len(ids))
	for _, id := range ids {
		if t, ok := m.tasks[id]; ok {
			cp := *t
			snap = append(snap, &cp)
		}
	}
	m.mu.Unlock()
	// 倒序：最新在前
	for i, j := 0, len(snap)-1; i < j; i, j = i+1, j-1 {
		snap[i], snap[j] = snap[j], snap[i]
	}
	return snap
}

// delete 删除任务：运行中先杀 ffmpeg；同时清理产物文件。
func (m *exportManager) delete(id string) (*ExportTask, bool) {
	m.mu.Lock()
	t, ok := m.tasks[id]
	if !ok {
		m.mu.Unlock()
		return nil, false
	}
	cp := *t
	if t.cmd != nil && t.cmd.Process != nil {
		_ = t.cmd.Process.Kill()
	}
	_ = os.Remove(filepath.Join(m.dir, "export_"+id+".mp4"))
	_ = os.Remove(filepath.Join(m.dir, "export_"+id+".txt"))
	delete(m.tasks, id)
	for i, oid := range m.order {
		if oid == id {
			m.order = append(m.order[:i], m.order[i+1:]...)
			break
		}
	}
	m.mu.Unlock()
	return &cp, true
}

func (m *exportManager) fail(t *ExportTask, msg string) {
	m.mu.Lock()
	if cur, ok := m.tasks[t.ID]; ok {
		cur.Status = "error"
		cur.Error = msg
	}
	m.mu.Unlock()
}

// run 执行导出：取相交分段 -> 写 concat 清单 -> ffmpeg -c copy 拼接。
func (m *exportManager) run(t *ExportTask) {
	m.mu.Lock()
	cur, ok := m.tasks[t.ID]
	if !ok || cur.Status != "pending" {
		m.mu.Unlock()
		return // 已被删除：直接退出
	}
	cur.Status = "running"
	outPath := filepath.Join(m.dir, "export_"+t.ID+".mp4")
	listPath := filepath.Join(m.dir, "export_"+t.ID+".txt")
	m.mu.Unlock()

	m.sem <- struct{}{}
	defer func() { <-m.sem }()

	// 逐天取分段（沿用按天查询的既有索引），合并后过滤相交段
	var segs []models.RecordingSegment
	for day := t.Start; !day.After(t.End); day = day.AddDate(0, 0, 1) {
		ds := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, day.Location())
		rows, err := m.st.SegmentsForDay(t.DeviceID, ds, ds.AddDate(0, 0, 1))
		if err == nil {
			segs = append(segs, rows...)
		}
	}
	var usable []models.RecordingSegment
	for _, s := range segs {
		if s.End.After(t.Start) && s.Start.Before(t.End) && fileExists(s.Path) {
			usable = append(usable, s)
		}
	}
	sort.Slice(usable, func(i, j int) bool { return usable[i].Start.Before(usable[j].Start) })
	if len(usable) == 0 {
		m.fail(t, "时间范围内没有录像文件（可能已被清理）")
		return
	}

	cuts := selectExportCuts(usable, t.Start, t.End)
	if err := writeConcatList(listPath, cuts); err != nil {
		m.fail(t, "写拼接清单失败: "+err.Error())
		return
	}

	args := []string{"-hide_banner", "-loglevel", "error", "-y",
		"-f", "concat", "-safe", "0", "-i", listPath,
		"-c", "copy", "-movflags", "+faststart", outPath}
	cmd := exec.Command(m.ffmpeg, args...)
	m.mu.Lock()
	if cur, ok := m.tasks[t.ID]; ok {
		cur.cmd = cmd
	}
	m.mu.Unlock()

	if out, err := cmd.CombinedOutput(); err != nil {
		_ = os.Remove(outPath)
		detail := strings.TrimSpace(string(out))
		if len(detail) > 200 {
			detail = detail[len(detail)-200:]
		}
		m.fail(t, fmt.Sprintf("ffmpeg 拼接失败: %v %s", err, detail))
		return
	}
	_ = os.Remove(listPath)

	fi, err := os.Stat(outPath)
	if err != nil {
		m.fail(t, "导出文件缺失")
		return
	}
	m.mu.Lock()
	if cur, ok := m.tasks[t.ID]; ok {
		cur.Status = "done"
		cur.file = outPath
		cur.Size = fi.Size()
	}
	m.mu.Unlock()
}

// exportCut 一段参与拼接的录像文件及其裁剪点（微秒）。
type exportCut struct {
	Path     string
	Inpoint  int64 // 0 = 从文件开头
	Outpoint int64 // 0 = 到文件末尾
}

// selectExportCuts 纯函数：返回与 [start,end] 相交的录像段及裁剪点（按开始时间排序）。
// 不做文件存在性检查（由调用方过滤），便于单元测试。
func selectExportCuts(segs []models.RecordingSegment, start, end time.Time) []exportCut {
	sorted := append([]models.RecordingSegment(nil), segs...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Start.Before(sorted[j].Start) })
	out := make([]exportCut, 0, len(sorted))
	for _, s := range sorted {
		if !s.End.After(start) || !s.Start.Before(end) {
			continue // 不相交（端点相触不算）
		}
		cut := exportCut{Path: s.Path}
		if s.Start.Before(start) {
			cut.Inpoint = start.Sub(s.Start).Microseconds()
		}
		if end.Before(s.End) {
			cut.Outpoint = end.Sub(s.Start).Microseconds()
		}
		out = append(out, cut)
	}
	return out

}

// writeConcatList 生成 ffmpeg concat demuxer 清单；绝对路径需 -safe 0。
func writeConcatList(path string, cuts []exportCut) error {
	var b strings.Builder
	b.WriteString("ffconcat version 1.0\n")
	for _, c := range cuts {
		// 单引号转义：concat demuxer 用单引号包裹路径
		escaped := strings.ReplaceAll(c.Path, "'", "'\\''")
		b.WriteString("file '" + escaped + "'\n")
		if c.Inpoint > 0 {
			b.WriteString(fmt.Sprintf("inpoint %d\n", c.Inpoint))
		}
		if c.Outpoint > 0 {
			b.WriteString(fmt.Sprintf("outpoint %d\n", c.Outpoint))
		}
	}
	return os.WriteFile(path, []byte(b.String()), 0o644)
}

// ---- HTTP handlers ----

type exportReq struct {
	Start int64 `json:"start"` // 毫秒时间戳
	End   int64 `json:"end"`   // 毫秒时间戳
}

func (s *Server) createExport(c *gin.Context) {
	deviceID := safePathID(c.Param("id"))
	var req exportReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "bad request body"})
		return
	}
	start := time.UnixMilli(req.Start)
	end := time.UnixMilli(req.End)
	task, err := s.exports.create(deviceID, start, end)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, task)
}

func (s *Server) listExports(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"tasks": s.exports.list()})
}

func (s *Server) deleteExport(c *gin.Context) {
	id := c.Param("tid")
	if _, ok := s.exports.delete(id); !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "task not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (s *Server) downloadExportFile(c *gin.Context) {
	id := c.Param("tid")
	t := s.exports.get(id)
	if t == nil || t.Status != "done" || t.file == "" {
		c.JSON(http.StatusNotFound, gin.H{"error": "export not ready"})
		return
	}
	if !fileExists(t.file) {
		c.JSON(http.StatusNotFound, gin.H{"error": "export file missing"})
		return
	}
	c.Header("Content-Disposition", "attachment; filename="+t.FileName)
	c.File(t.file)
}
