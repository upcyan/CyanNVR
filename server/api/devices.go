package api

import (
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
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

	RecordEnabled   *bool           `json:"recordEnabled"`
	RecordMode      string          `json:"recordMode"`
	RetentionDays   *int            `json:"retentionDays"`
	RetentionSizeGB *int            `json:"retentionSizeGB"`
	ScheduleStart   string          `json:"scheduleStart"`
	ScheduleEnd     string          `json:"scheduleEnd"`
	AIEnabled       *bool           `json:"aiEnabled"`
	Streams         []models.Stream `json:"streams"`
	PreviewStream   string          `json:"previewStream"`
	RecordStream    string          `json:"recordStream"`
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
	// 重复添加检测：同一台摄像头（IP+端口，或归一化后的 RTSP 地址）存两条会
	// 重复占用相机的并发取流路数（常见仅 3~6 路），最终两边都黑屏。
	// 未带 force=true 时只警告不拦截——确实需要两路（不同账号/路径）可二次确认。
	if src == models.SourceRTSP && c.Query("force") != "true" {
		if dupName := s.findDuplicateDevice(req, ""); dupName != "" {
			c.JSON(http.StatusConflict, gin.H{
				"error":    "疑似重复添加：与已有设备「" + dupName + "」的 IP+端口（或 RTSP 地址）相同",
				"code":     "duplicate_device",
				"conflict": dupName,
			})
			return
		}
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
	// 编辑也可能把地址改成与另一台相同，一并查重（排除自身）。
	if d.Source == models.SourceRTSP && c.Query("force") != "true" {
		if dupName := s.findDuplicateDeviceFor(d); dupName != "" {
			c.JSON(http.StatusConflict, gin.H{
				"error":    "疑似重复添加：与已有设备「" + dupName + "」的 IP+端口（或 RTSP 地址）相同",
				"code":     "duplicate_device",
				"conflict": dupName,
			})
			return
		}
	}
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
	// probeURL 测试一条 RTSP 地址并组装响应：连通性之外附带编码/分辨率
	// 与 H.265 提示，让「测试连接」一步就能发现浏览器播不了的摄像头。
	probeURL := func(u string) (gin.H, string) {
		ok, errMsg, codec, width, height := ffmpeg.TestRTSPProbe(s.cfg.Ffmpeg, u)
		resp := gin.H{"ok": ok, "url": u}
		if codec != "" {
			resp["codec"] = codec
		}
		if width > 0 && height > 0 {
			resp["width"] = width
			resp["height"] = height
		}
		if ok && codec != "" && codec != "h264" {
			resp["h265"] = codec == "hevc"
			resp["advice"] = "该码流为 " + codecName(codec) +
				"，浏览器无法直接播放（录像会自动转码，但更耗资源）。" +
				"建议到摄像头后台「配置 → 视频/音频 → 视频」把主/子码流都改为 H.264。"
		}
		return resp, errMsg
	}
	if url := req.RTSPURL; url != "" {
		resp, errMsg := probeURL(url)
		if !resp["ok"].(bool) {
			resp["error"] = errMsg
		}
		c.JSON(http.StatusOK, resp)
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
		resp, errMsg := probeURL(u)
		if resp["ok"].(bool) {
			c.JSON(http.StatusOK, resp)
			return
		}
		lastErr = errMsg
		// 主机级失败（网络不可达/被拒/超时/认证失败）换路径也没用——
		// 问题不在 URL 路径上。继续试剩余 6 条只会把等待时间翻好几倍，
		// 这里直接返回首次的明确原因。
		if isHostLevelProbeErr(errMsg) {
			resp["error"] = errMsg
			c.JSON(http.StatusOK, resp)
			return
		}
	}
	c.JSON(http.StatusOK, gin.H{
		"ok":    false,
		"url":   buildRTSPURL(req.IP, port, req.Username, req.Password, "/stream1"),
		"error": lastErr,
	})
}

// isHostLevelProbeErr 判断探测错误是否属于「主机/凭据层面」——
// 这类错误与具体码流路径无关，逐条换路径重试没有意义。
func isHostLevelProbeErr(msg string) bool {
	for _, k := range []string{
		"网络不可达", "连接被拒绝", "连接超时", "探测超时", "认证失败",
		"ffmpeg 未安装", "地址格式错误",
	} {
		if strings.Contains(msg, k) {
			return true
		}
	}
	return false
}

// findDuplicateDevice 判断「将要创建的设备」是否与已有设备指向同一台摄像头。
func (s *Server) findDuplicateDevice(req deviceReq, excludeID string) string {
	prov := models.Device{
		ID:       excludeID,
		IP:       req.IP,
		Port:     req.Port,
		Username: req.Username,
		RTSPURL:  req.RTSPURL,
		Streams:  req.Streams,
		Source:   models.SourceRTSP,
	}
	return s.findDuplicateDeviceFor(&prov)
}

// findDuplicateDeviceFor 以设备对象比对，供编辑（update）流程排除自身后查重。
func (s *Server) findDuplicateDeviceFor(d *models.Device) string {
	devs, err := s.st.ListDevices()
	if err != nil {
		return ""
	}
	mine := deviceIdentityKeys(d)
	if len(mine) == 0 {
		return ""
	}
	for _, other := range devs {
		if other.ID == d.ID || other.Source != models.SourceRTSP {
			continue
		}
		if keysIntersect(mine, deviceIdentityKeys(&other)) {
			return other.Name
		}
	}
	return ""
}

// deviceIdentityKeys 返回一台设备的全部「身份键」，用于查重：
//
//	host:<ip>:<port>              同 IP+端口（NVR 多通道也共享同一取流会话上限，
//	                              同地址同样值得提醒）
//	rtsp://<host>:<port>/<path>   归一化取流地址（去凭证/query；路径才是真正的
//	                              码流标识，可区分同一台 NVR 的不同通道）
//
// 之所以返回的是集合而不是单个值：新设备可能只填了 IP（键只有 host:…），而库里
// 已有设备带完整 RTSP 地址（键是 rtsp://…），用单值比较会永远判不出重复。
func deviceIdentityKeys(d *models.Device) []string {
	if d.Source == models.SourceTest {
		return nil
	}
	seen := map[string]bool{}
	add := func(k string) {
		if k != "" {
			seen[k] = true
		}
	}
	port := d.Port
	if port == 0 {
		port = 554
	}
	if d.IP != "" {
		add("host:" + net.JoinHostPort(d.IP, strconv.Itoa(port)))
	}
	for _, st := range d.Streams {
		add(normalizeRTSP(st.URL))
	}
	if d.RTSPURL != "" {
		add(normalizeRTSP(d.RTSPURL))
	} else if d.IP != "" {
		// 未填显式地址时，按录像实际会用的默认地址参与比对，
		// 否则「只填 IP 再加一次」永远匹配不上已有设备。
		add(normalizeRTSP(defaultRTSP(d)))
	}
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	return out
}

// keysIntersect 判断两组身份键是否有交集。
func keysIntersect(a, b []string) bool {
	if len(a) == 0 || len(b) == 0 {
		return false
	}
	set := make(map[string]bool, len(a))
	for _, k := range a {
		set[k] = true
	}
	for _, k := range b {
		if set[k] {
			return true
		}
	}
	return false
}

// normalizeRTSP 把 RTSP 地址规整成可比较的 key：
// 去掉 userinfo、query、fragment，host 小写、path 去掉末尾斜杠。
// 这样 "rtsp://User:Pass@Cam/stream1" 与 "rtsp://cam/stream1" 判为同一台。
func normalizeRTSP(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return ""
	}
	u.User = nil
	u.RawQuery = ""
	u.Fragment = ""
	u.Scheme = "rtsp"
	u.Host = strings.ToLower(u.Host)
	u.Path = strings.TrimSuffix(u.Path, "/")
	if u.Path == "" {
		u.Path = "/stream1"
	}
	return u.String()
}

// codecName 把 ffmpeg 编码标识翻成用户看得懂的名字。
func codecName(codec string) string {
	switch codec {
	case "hevc":
		return "H.265(HEVC)"
	case "h264":
		return "H.264"
	case "mpeg4":
		return "MPEG-4"
	case "vp9":
		return "VP9"
	case "vp8":
		return "VP8"
	case "av1":
		return "AV1"
	}
	return strings.ToUpper(codec)
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

// deviceSnapshot 返回摄像机当前画面（JPEG）。
//
// 行为：优先复用 3 秒内的 current.jpg（避免客户端轮询时反复起 ffmpeg）；
// 文件不存在或已过期时，按需抓一帧（AI 关闭时不会有常驻抓帧进程，
// 这个接口是唯一的取图途径）。并发请求由 recorder 侧合并为一次抓帧。
func (s *Server) deviceSnapshot(c *gin.Context) {
	id := safePathID(c.Param("id"))
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid device id"})
		return
	}
	// 设备必须存在，否则会为不存在的 ID 白白起 ffmpeg
	if d, err := s.st.GetDevice(id); err != nil || d == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "device not found"})
		return
	}
	path, err := s.rec.GrabSnapshot(id, 3*time.Second)
	if err != nil || path == "" {
		msg := "no snapshot"
		if err != nil {
			msg = err.Error()
		}
		c.JSON(http.StatusNotFound, gin.H{"error": msg})
		return
	}
	// 快照是实时画面，禁止中间层缓存，否则客户端会一直看到旧帧
	c.Header("Cache-Control", "no-store")
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
		Date     string `json:"date"`
		Has      bool   `json:"hasRecording"`
		Duration int    `json:"duration"`
		Segments int    `json:"segments"`
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
