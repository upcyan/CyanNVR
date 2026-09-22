package api

import (
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"cyannvr/server/models"
	"cyannvr/server/pkg/ffmpeg"
	"cyannvr/server/pkg/onvifx"
)

func (s *Server) listDevices(c *gin.Context) {
	devs, err := s.st.ListDevices()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	// Strip passwords from API response.
	safe := make([]models.Device, len(devs))
	copy(safe, devs)
	for i := range safe {
		safe[i].Password = ""
	}
	c.JSON(http.StatusOK, gin.H{"devices": safe})
}

type deviceReq struct {
	Name     string `json:"name"`
	IP       string `json:"ip"`
	Port     int    `json:"port"`
	Username string `json:"username"`
	Password string `json:"password"`
	Source   string `json:"source"` // rtsp | test
	RTSPURL  string `json:"rtspUrl"`

	RecordEnabled   *bool `json:"recordEnabled"`
	RecordMode      string          `json:"recordMode"`
	RetentionDays   *int            `json:"retentionDays"`
	RetentionSizeGB *int            `json:"retentionSizeGB"`
	ScheduleStart string          `json:"scheduleStart"`
	ScheduleEnd   string          `json:"scheduleEnd"`
	AIEnabled     *bool           `json:"aiEnabled"`
	Streams       []models.Stream `json:"streams"`
	PreviewStream string          `json:"previewStream"`
	RecordStream  string          `json:"recordStream"`
}

func (r *deviceReq) applyTo(d *models.Device) {
	if r.RecordEnabled != nil {
		d.RecordEnabled = *r.RecordEnabled
	}
	if r.RecordMode != "" {
		d.RecordMode = r.RecordMode
	}
	if r.ScheduleStart != "" {
		d.ScheduleStart = r.ScheduleStart
	}
	if r.ScheduleEnd != "" {
		d.ScheduleEnd = r.ScheduleEnd
	}
	if r.RetentionDays != nil {
		d.RetentionDays = *r.RetentionDays
	}
	if r.RetentionSizeGB != nil {
		d.RetentionSizeGB = *r.RetentionSizeGB
	}
	if r.AIEnabled != nil {
		d.AIEnabled = r.AIEnabled
	}
	if r.Streams != nil {
		d.Streams = r.Streams
	}
	if r.PreviewStream != "" {
		d.PreviewStream = r.PreviewStream
	}
	if r.RecordStream != "" {
		d.RecordStream = r.RecordStream
	}
}

func (s *Server) createDevice(c *gin.Context) {
	var req deviceReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}
	src := models.SourceRTSP
	if req.Source == "test" {
		src = models.SourceTest
	}
	if src == models.SourceRTSP && req.IP == "" && req.RTSPURL == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "需要 IP 或 RTSP 地址"})
		return
	}
	if req.Port == 0 {
		req.Port = 554
	}
	name := req.Name
	if name == "" {
		name = req.IP
	}
	d := models.Device{
		ID:            uuid.NewString(),
		Name:          name,
		IP:            req.IP,
		Port:          req.Port,
		Username:      req.Username,
		Password:      req.Password,
		Source:        src,
		Model:         "RTSP Camera",
		RecordEnabled: true,
		RecordMode:    "continuous",
		ScheduleStart: "08:00",
		ScheduleEnd:   "20:00",
		Created:       time.Now(),
	}
	if req.RTSPURL != "" {
		d.RTSPURL = req.RTSPURL
		d.Model = "ONVIF/RTSP"
	}
	req.applyTo(&d)
	if err := s.st.CreateDevice(d); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	s.rec.StartWorker(&d)
	d.Password = "" // Do not return password in response
	c.JSON(http.StatusOK, gin.H{"device": d})
}

func (s *Server) deleteDevice(c *gin.Context) {
	id := c.Param("id")
	s.rec.StopWorker(id)
	if err := s.st.DeleteDevice(id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (s *Server) updateDevice(c *gin.Context) {
	id := c.Param("id")
	d, err := s.st.GetDevice(id)
	if err != nil || d == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "device not found"})
		return
	}
	var req deviceReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}
	if req.Name != "" {
		d.Name = req.Name
	}
	if req.IP != "" {
		d.IP = req.IP
	}
	if req.Port != 0 {
		d.Port = req.Port
	}
	if req.Username != "" {
		d.Username = req.Username
	}
	if req.Password != "" {
		d.Password = req.Password
	}
	if req.RTSPURL != "" {
		d.RTSPURL = req.RTSPURL
	}
	req.applyTo(d)
	if err := s.st.UpdateDevice(*d); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	s.rec.Restart(d)
	d.Password = "" // Do not return password in response
	c.JSON(http.StatusOK, gin.H{"device": d})
}

func (s *Server) testDevice(c *gin.Context) {
	var req struct {
		IP       string `json:"ip"`
		Port     int    `json:"port"`
		Username string `json:"username"`
		Password string `json:"password"`
		RTSPURL  string `json:"rtspUrl"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}
	url := req.RTSPURL
	if url != "" {
		ok, errMsg := ffmpeg.TestRTSPErr(s.cfg.Ffmpeg, url)
		if ok {
			c.JSON(http.StatusOK, gin.H{"ok": true, "url": url})
		} else {
			c.JSON(http.StatusOK, gin.H{"ok": false, "url": url, "error": errMsg})
		}
		return
	}
	port := req.Port
	if port == 0 {
		port = 554
	}
	// Try common RTSP paths across camera brands. Remembers which one worked
	// so the device can be stored with the correct URL.
	var lastErr string
	for _, path := range []string{
		"/stream1",
		"/ch1/main",
		"/cam/realmonitor?channel=1&subtype=0",
		"/Streaming/Channels/101",
		"/live",
		"/h264/ch1/main/av_stream",
		"/main",
	} {
		u := buildRTSPURL(req.IP, port, req.Username, req.Password, path)
		ok, errMsg := ffmpeg.TestRTSPErr(s.cfg.Ffmpeg, u)
		if ok {
			c.JSON(http.StatusOK, gin.H{"ok": true, "url": u})
			return
		}
		lastErr = errMsg
	}
	c.JSON(http.StatusOK, gin.H{
		"ok":    false,
		"url":   buildRTSPURL(req.IP, port, req.Username, req.Password, "/stream1"),
		"error": lastErr,
	})
}

func buildRTSPURL(ip string, port int, user, pass, path string) string {
	u := url.URL{Scheme: "rtsp", Host: net.JoinHostPort(ip, strconv.Itoa(port)), Path: path}
	if user != "" {
		u.User = url.UserPassword(user, pass)
	}
	return u.String()
}

func (s *Server) discoverDevices(c *gin.Context) {
	var req struct {
		Interface string `json:"interface"`
	}
	_ = c.ShouldBindJSON(&req)

	// 1) ONVIF 标准发现（WS-Discovery 广播）
	found, err := onvifx.Discover(c.Request.Context(), req.Interface)
	if err != nil {
		// ONVIF 失败不应阻断小米摄像头的主动扫描
		log.Printf("onvif discover: %v", err)
		found = nil
	}

	// 2) 小米摄像头主动扫描
	//    多数小米摄像头不响应 ONVIF 广播（或其 ONVIF 默认关闭），
	//    故按其 RTSP 特征端口 8554 主动探测，两者结果合并去重。
	xiaomi := onvifx.DiscoverXiaomi(c.Request.Context())

	c.JSON(http.StatusOK, gin.H{"devices": mergeDiscovered(found, xiaomi)})
}

// mergeDiscovered 合并两路发现结果，按 IP:Port 去重（ONVIF 结果优先）。
func mergeDiscovered(onvifFound, xiaomiFound []onvifx.Found) []onvifx.Found {
	seen := make(map[string]bool, len(onvifFound)+len(xiaomiFound))
	out := make([]onvifx.Found, 0, len(onvifFound)+len(xiaomiFound))
	for _, group := range [][]onvifx.Found{onvifFound, xiaomiFound} {
		for _, f := range group {
			key := net.JoinHostPort(f.IP, strconv.Itoa(f.Port))
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, f)
		}
	}
	return out
}

func (s *Server) probeDevice(c *gin.Context) {
	id := c.Param("id")
	d, err := s.st.GetDevice(id)
	if err != nil || d == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "device not found"})
		return
	}
	stream := ""
	if d.Source != models.SourceTest {
		streams := onvifx.GetStreams(d.IP, d.Port, d.Username, d.Password)
		if len(streams) > 0 {
			d.Streams = streams
			if d.PreviewStream == "" {
				d.PreviewStream = streams[0].ID
			}
			if d.RecordStream == "" {
				d.RecordStream = streams[0].ID
			}
			stream = streams[0].URL
		}
		if stream == "" {
			stream = defaultRTSP(d)
		}
		d.RTSPURL = stream
		d.Model = "ONVIF/RTSP"
		_ = s.st.UpdateDevice(*d)
	}
	c.JSON(http.StatusOK, gin.H{"device": d, "stream": stream})
}

// probeStreams fetches the list of video streams (ONVIF profiles) a camera
// offers, given connection details from the add-device form.
func (s *Server) probeStreams(c *gin.Context) {
	var req struct {
		IP       string `json:"ip"`
		Port     int    `json:"port"`
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}
	port := req.Port
	if port == 0 {
		port = 554
	}
	streams := onvifx.GetStreams(req.IP, port, req.Username, req.Password)
	if len(streams) == 0 {
		// Fall back to the default single stream so the UI still works.
		u := url.URL{Scheme: "rtsp", Host: net.JoinHostPort(req.IP, "554"), Path: "/stream1"}
		if req.Username != "" {
			u.User = url.UserPassword(req.Username, req.Password)
		}
		streams = []models.Stream{{ID: "main", Name: "主码流", URL: u.String()}}
	}
	c.JSON(http.StatusOK, gin.H{"streams": streams})
}

func defaultRTSP(d *models.Device) string {
	if d.RTSPURL != "" {
		return d.RTSPURL
	}
	port := d.Port
	if port == 0 {
		port = 554
	}
	u := url.URL{Scheme: "rtsp", Host: net.JoinHostPort(d.IP, strconv.Itoa(port)), Path: "/stream1"}
	if d.Username != "" {
		u.User = url.UserPassword(d.Username, d.Password)
	}
	return u.String()
}

func (s *Server) deviceSnapshot(c *gin.Context) {
	id := safePathID(c.Param("id"))
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid device id"})
		return
	}
	path := filepath.Join(s.cfg.SnapDir, id, "current.jpg")
	if !fileExists(path) {
		c.JSON(http.StatusNotFound, gin.H{"error": "no snapshot"})
		return
	}
	c.File(path)
}

func (s *Server) deviceRecordings(c *gin.Context) {
	id := c.Param("id")
	// 校验设备存在：原先对不存在的设备也返回 200 + 空列表，
	// 调用方无法区分"设备不存在"与"当日无录像"
	if d, err := s.st.GetDevice(id); err != nil || d == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "device not found"})
		return
	}
	dateStr := c.Query("date")
	if dateStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "date required"})
		return
	}
	day, err := time.ParseInLocation("2006-01-02", dateStr, time.Local)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "bad date"})
		return
	}
	segs, err := s.st.SegmentsForDay(id, day, day.AddDate(0, 0, 1))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"segments": segs})
}

func (s *Server) deviceMonth(c *gin.Context) {
	id := c.Param("id")
	ym := c.Query("ym")
	if len(ym) != 7 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ym required (YYYY-MM)"})
		return
	}
	year := atoiStr(ym[:4])
	month := atoiStr(ym[5:7])
	first := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, time.Local)
	last := first.AddDate(0, 1, 0)
	type dayInfo struct {
		Date       string `json:"date"`
		Has        bool   `json:"hasRecording"`
		Duration   int    `json:"duration"`
		Segments   int    `json:"segments"`
	}
	out := []dayInfo{}
	for day := first; day.Before(last); day = day.AddDate(0, 0, 1) {
		next := day.AddDate(0, 0, 1)
		segs, err := s.st.SegmentsForDay(id, day, next)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		dur := 0
		for _, sg := range segs {
			dur += int(sg.End.Sub(sg.Start).Minutes())
		}
		out = append(out, dayInfo{
			Date:     day.Format("2006-01-02"),
			Has:      len(segs) > 0,
			Duration: dur,
			Segments: len(segs),
		})
	}
	c.JSON(http.StatusOK, gin.H{"days": out})
}

func (s *Server) createPlayback(c *gin.Context) {
	id := c.Param("id")
	var req struct {
		Start time.Time `json:"start"`
		End   time.Time `json:"end"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Start.IsZero() || req.End.IsZero() {
		c.JSON(http.StatusBadRequest, gin.H{"error": "start/end required"})
		return
	}
	if req.End.Before(req.Start) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "bad range"})
		return
	}
	d, err := s.st.GetDevice(id)
	if err != nil || d == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "device not found"})
		return
	}
	sess, err := s.hls.CreatePlayback(id, req.Start, req.End, s.playbackNeedsTranscode(d))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"url": "/api/stream/playback/" + sess.Name + "/index.m3u8", "session": sess.Name})
}

// playbackNeedsTranscode reports whether recorded segments must be re-encoded
// to h264 so the browser can play them back (e.g. HEVC cameras).
func (s *Server) playbackNeedsTranscode(d *models.Device) bool {
	if d.Source == models.SourceTest {
		return true
	}
	for _, url := range []string{s.recStreamURL(d), d.RTSPURL} {
		if url == "" {
			continue
		}
		codec := ffmpeg.ProbeVideoCodec(s.cfg.Ffmpeg, url)
		if codec != "" && codec != "h264" {
			return true
		}
	}
	return false
}

func (s *Server) recStreamURL(d *models.Device) string {
	if d.RecordStream != "" {
		for _, st := range d.Streams {
			if st.ID == d.RecordStream && st.URL != "" {
				return st.URL
			}
		}
	}
	if d.RTSPURL != "" {
		return d.RTSPURL
	}
	if d.Username != "" {
		return fmt.Sprintf("rtsp://%s:%s@%s:%d/stream1", d.Username, d.Password, d.IP, d.Port)
	}
	return fmt.Sprintf("rtsp://%s:%d/stream1", d.IP, d.Port)
}

func atoiStr(s string) int {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			continue
		}
		n = n*10 + int(r-'0')
	}
	return n
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// safePathID validates that a user-supplied ID contains only safe characters
// and cannot be used for path traversal. Returns the cleaned ID or empty string.
func safePathID(id string) string {
	if id == "" || len(id) > 128 {
		return ""
	}
	for _, c := range id {
		if c == '/' || c == '\\' || c == '.' || c == '\x00' {
			return ""
		}
	}
	return id
}
