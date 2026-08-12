package ai

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/google/uuid"

	"simplenvr/server/config"
	"simplenvr/server/models"
	"simplenvr/server/pkg/gif"
	"simplenvr/server/pkg/snapshot"
	"simplenvr/server/store"
)

type Analyzer struct {
	cfg       *config.Config
	st        *store.Store
	http      *http.Client
	dirs      map[string]*DeviceState
	onEvent   func(eventType, deviceID, deviceName, eventID, label, desc, time string)
}

type DeviceState struct {
	ring       *snapshot.Ring
	lastEvent  time.Time
	lastCheck  time.Time
	lock       chan struct{} // 1-buffered, serializes per-device analysis
}

func (ds *DeviceState) LastEventTime() time.Time { return ds.lastEvent }

func New(cfg *config.Config, st *store.Store, onEvent func(string, string, string, string, string, string, string)) *Analyzer {
	return &Analyzer{
		cfg:     cfg,
		st:      st,
		http:    &http.Client{Timeout: 30 * time.Second},
		dirs:    map[string]*DeviceState{},
		onEvent: onEvent,
	}
}

// StartLocalWorker launches the bundled Python detection worker if the detect
// endpoint is not already reachable and a python interpreter is available.
// Non-fatal: AI simply reports errors if the worker is missing.
func StartLocalWorker(detectURL string) {
	if detectURL == "" {
		return
	}
	u := strings.TrimRight(detectURL, "/") + "/"
	client := &http.Client{Timeout: 2 * time.Second}
	if resp, err := client.Get(u); err == nil {
		resp.Body.Close()
		log.Printf("AI detect worker already running at %s", detectURL)
		return
	}
	script := os.Getenv("NVR_AI_DETECT_SCRIPT")
	if script == "" {
		// Bundled script lives next to this package.
		_, file, _, _ := runtime.Caller(0)
		script = filepath.Join(filepath.Dir(file), "ai_detect.py")
	}
	if _, err := os.Stat(script); err != nil {
		log.Printf("AI detect worker script not found: %s", script)
		return
	}
	py := os.Getenv("PYTHON")
	if py == "" {
		py = "python"
	}
	port := "11435"
	if i := strings.LastIndex(u, ":"); i >= 0 {
		rest := strings.TrimSuffix(u[i+1:], "/")
		if rest != "" {
			port = rest
		}
	}
	cmd := exec.Command(py, script, port)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		log.Printf("start AI detect worker: %v", err)
		return
	}
	log.Printf("started AI detect worker (pid %d) at %s", cmd.Process.Pid, detectURL)
}

func (a *Analyzer) Register(deviceID string) *DeviceState {
	st := &DeviceState{
		ring: snapshot.NewRing(8),
		lock: make(chan struct{}, 1),
	}
	st.lock <- struct{}{}
	a.dirs[deviceID] = st
	return st
}

func (a *Analyzer) State(deviceID string) *DeviceState { return a.dirs[deviceID] }

// PushImage feeds a frame captured at time t into the device's ring.
func (a *Analyzer) PushImage(deviceID string, data []byte, t int64) {
	if s, ok := a.dirs[deviceID]; ok {
		s.ring.Push(data, t)
	}
}

// MaybeAnalyze triggers analysis for a device if interval elapsed.
func (a *Analyzer) MaybeAnalyze(device *models.Device) {
	// Device-level toggle wins; when unset, fall back to the global setting.
	if device.AIEnabled != nil {
		if !*device.AIEnabled {
			return
		}
	} else if !a.cfg.AIEnabled {
		return
	}
	if a.cfg.AIBaseURL == "" {
		return
	}
	s := a.dirs[device.ID]
	if s == nil {
		return
	}
	now := time.Now()
	if now.Sub(s.lastCheck) < time.Duration(a.cfg.SnapshotIntervalSec)*time.Second {
		return
	}
	s.lastCheck = now
	if now.Sub(s.lastEvent) < time.Duration(a.cfg.AICooldown)*time.Second {
		return
	}
	select {
	case <-s.lock:
		go func() {
			defer func() { s.lock <- struct{}{} }()
			a.analyze(device, s)
		}()
	default:
	}
}

type result struct {
	Alert       bool    `json:"alert"`
	Label       string  `json:"label"`
	Description string  `json:"description"`
	Score       float64 `json:"score"`
}

func (a *Analyzer) analyze(device *models.Device, s *DeviceState) {
	frames := s.ring.Snapshot(20000, 1) // newest frame within 20s
	if len(frames) == 0 {
		return
	}
	var desc result
	var score float64
	var err error
	if a.cfg.AIMode == "local" {
		desc, score, err = a.detectLocal(frames[0])
	} else {
		desc, score, err = a.describe(frames[0])
	}
	if err != nil {
		return
	}
	if desc.Alert && score >= a.cfg.AIThreshold && desc.Label != "" {
		a.emitEvent(device, s, desc, score)
	}
}

// detectObject is the response item from the local detect worker.
type detectObject struct {
	Label      string  `json:"label"`
	Confidence float64 `json:"confidence"`
	Box        []int   `json:"box"`
}

// detectLocal runs on-device object detection (Frigate-style) by posting the
// frame to a local OpenCV worker. Any detected object raises an alert; the
// highest-confidence label becomes the event label.
func (a *Analyzer) detectLocal(jpg []byte) (result, float64, error) {
	if a.cfg.AIDetectURL == "" {
		return result{}, 0, fmt.Errorf("no detect url")
	}
	payload := map[string]any{"image": base64.StdEncoding.EncodeToString(jpg)}
	body, _ := json.Marshal(payload)
	url := strings.TrimRight(a.cfg.AIDetectURL, "/") + "/"
	req, err := http.NewRequest("POST", url, bytes.NewReader(body))
	if err != nil {
		return result{}, 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := a.http.Do(req)
	if err != nil {
		return result{}, 0, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil || resp.StatusCode != 200 {
		return result{}, 0, fmt.Errorf("detect http %d", resp.StatusCode)
	}
	var out struct {
		Objects []detectObject `json:"objects"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return result{}, 0, err
	}
	if len(out.Objects) == 0 {
		return result{Alert: false}, 0, nil
	}
	// Pick the highest-confidence detection.
	best := out.Objects[0]
	for _, o := range out.Objects[1:] {
		if o.Confidence > best.Confidence {
			best = o
		}
	}
	label := mapLabel(best.Label)
	desc := fmt.Sprintf("检测到%s（置信度 %.0f%%）", label, best.Confidence*100)
	return result{Alert: true, Label: label, Description: desc, Score: best.Confidence}, best.Confidence, nil
}

var labelMap = map[string]string{
	"person": "人员",
	"car":    "车辆",
	"truck":  "卡车",
	"bus":    "客车",
	"dog":    "犬只",
	"cat":    "猫",
	"bicycle": "自行车",
	"motorcycle": "摩托车",
}

func mapLabel(l string) string {
	if v, ok := labelMap[l]; ok {
		return v
	}
	return l
}

func (a *Analyzer) emitEvent(device *models.Device, s *DeviceState, r result, score float64) {
	now := time.Now()
	s.lastEvent = now
	evID := uuid.NewString()
	dir := fmt.Sprintf("%s/%s/%s", a.cfg.EventDir, device.ID, evID)
	if err := mkdirAll(dir); err != nil {
		return
	}

	snapPath := dir + "/snapshot.jpg"
	frames := s.ring.Snapshot(25000, 8)
	if len(frames) > 0 {
		_ = writeFile(snapPath, frames[len(frames)-1])
	}
	var gifPath string
	if len(frames) >= 2 {
		if g, err := gif.Build(frames, 250); err == nil && len(g) > 0 {
			gifPath = dir + "/animation.gif"
			_ = writeFile(gifPath, g)
		}
	}

	label := r.Label
	if r.Label == "" {
		label = "AI 事件"
	}
	desc := r.Description
	if desc == "" {
		desc = fmt.Sprintf("AI 识别：%s（置信度 %.0f%%）", label, score*100)
	}

	ev := models.Event{
		ID:         evID,
		DeviceID:   device.ID,
		DeviceName: device.Name,
		Type:       models.EventAI,
		Label:      label,
		Desc:       desc,
		Time:       now,
	}
	if snapPath != "" {
		ev.Snapshot = fmt.Sprintf("/api/events/%s/snapshot", evID)
	}
	if gifPath != "" {
		ev.GIF = fmt.Sprintf("/api/events/%s/gif", evID)
	}
	if vs, ve, ok := a.segmentRange(device.ID, now); ok {
		ev.VideoStart, ev.VideoEnd = vs, ve
	}
	_ = a.st.CreateEvent(ev)
	if a.onEvent != nil {
		a.onEvent(string(ev.Type), device.ID, device.Name, evID, label, desc, now.Format(time.RFC3339))
	}
}

func (a *Analyzer) segmentRange(deviceID string, t time.Time) (*time.Time, *time.Time, bool) {
	dayStart := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
	dayEnd := dayStart.AddDate(0, 0, 1)
	segs, err := a.st.SegmentsForDay(deviceID, dayStart, dayEnd)
	if err != nil {
		return nil, nil, false
	}
	for _, s := range segs {
		if t.After(s.Start) && t.Before(s.End) {
			vs := t.Add(-10 * time.Second)
			ve := t.Add(10 * time.Second)
			if vs.Before(s.Start) {
				vs = s.Start
			}
			if ve.After(s.End) {
				ve = s.End
			}
			return &vs, &ve, true
		}
	}
	return nil, nil, false
}

// describe calls an OpenAI-compatible vision endpoint and parses JSON result.
func (a *Analyzer) describe(jpg []byte) (result, float64, error) {
	payload := map[string]any{
		"model": a.cfg.AIModel,
		"messages": []any{
			map[string]any{
				"role": "user",
				"content": []any{
					map[string]any{"type": "text", "text": a.cfg.AIPrompt},
					map[string]any{
						"type": "image_url",
						"image_url": map[string]any{
							"url": "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(jpg),
						},
					},
				},
			},
		},
		"max_tokens": 300,
	}
	body, _ := json.Marshal(payload)
	url := strings.TrimRight(a.cfg.AIBaseURL, "/") + "/chat/completions"
	req, err := http.NewRequest("POST", url, bytes.NewReader(body))
	if err != nil {
		return result{}, 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	if a.cfg.AIAPIKey != "" {
		req.Header.Set("Authorization", "Bearer "+a.cfg.AIAPIKey)
	}
	resp, err := a.http.Do(req)
	if err != nil {
		return result{}, 0, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil || resp.StatusCode != 200 {
		return result{}, 0, fmt.Errorf("ai http %d", resp.StatusCode)
	}
	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return result{}, 0, err
	}
	content := ""
	if len(out.Choices) > 0 {
		content = out.Choices[0].Message.Content
	}
	content = extractJSON(content)
	var r result
	if err := json.Unmarshal([]byte(content), &r); err != nil {
		// non-JSON response: treat as alert with raw text
		if strings.TrimSpace(content) != "" {
			return result{Alert: true, Label: "AI 识别", Description: content, Score: 0.9}, 0.9, nil
		}
		return result{}, 0, err
	}
	if r.Score == 0 && r.Alert {
		r.Score = 0.8
	}
	return r, r.Score, nil
}

func extractJSON(s string) string {
	start := strings.Index(s, "{")
	end := strings.LastIndex(s, "}")
	if start < 0 || end < start {
		return strings.TrimSpace(s)
	}
	return s[start : end+1]
}

func mkdirAll(p string) error    { return os.MkdirAll(p, 0o755) }
func writeFile(p string, b []byte) error { return os.WriteFile(p, b, 0o644) }
