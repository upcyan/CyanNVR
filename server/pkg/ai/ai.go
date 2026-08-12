package ai

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
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
	if !a.cfg.AIEnabled || a.cfg.AIBaseURL == "" {
		return
	}
	if device.AIEnabled != nil && !*device.AIEnabled {
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
	desc, score, err := a.describe(frames[0])
	if err != nil {
		return
	}
	if desc.Alert && score >= a.cfg.AIThreshold && desc.Label != "" {
		a.emitEvent(device, s, desc, score)
	}
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
