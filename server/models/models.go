package models

import "time"

type Role string

const (
	RoleAdmin   Role = "admin"
	RoleUser    Role = "user"
	RoleViewer  Role = "viewer"
	RoleOperator Role = "operator"
)

type User struct {
	ID           string    `json:"id"`
	Username     string    `json:"username"`
	PasswordHash string    `json:"-"`
	Role         Role      `json:"role"`
	CreatedAt    time.Time `json:"createdAt"`
}

type DeviceSource string

const (
	SourceRTSP DeviceSource = "rtsp"
	SourceTest DeviceSource = "test"
)

type Device struct {
	ID       string       `json:"id"`
	Name     string       `json:"name"`
	IP       string       `json:"ip"`
	Port     int          `json:"port"`
	Username string       `json:"username"`
	Password string       `json:"password,omitempty"` // Hidden by default, only set via dedicated field
	Source   DeviceSource `json:"source"`
	Model    string       `json:"model"`
	Online   bool         `json:"online"`
	RTSPURL  string       `json:"rtspUrl,omitempty"`
	Created  time.Time    `json:"created"`

	// Per-device recording strategy. When RecordEnabled is false the device
	// is only live-streamed, never recorded.
	RecordEnabled bool   `json:"recordEnabled"`
	RecordMode    string `json:"recordMode"` // continuous | motion | schedule
	ScheduleStart string `json:"scheduleStart"`
	ScheduleEnd   string `json:"scheduleEnd"`

	// Per-device AI analysis override (defaults to global AI setting when unset).
	AIEnabled *bool `json:"aiEnabled,omitempty"`

	// Multiple video streams (ONVIF profiles). PreviewStream / RecordStream
	// select which stream is used for live view and storage respectively.
	Streams       []Stream `json:"streams,omitempty"`
	PreviewStream string   `json:"previewStream,omitempty"`
	RecordStream  string   `json:"recordStream,omitempty"`
}

// Stream describes one video profile offered by a camera.
type Stream struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	URL  string `json:"url"`
}

type RecordingSegment struct {
	ID       string    `json:"id"`
	DeviceID string    `json:"deviceId"`
	Start    time.Time `json:"start"`
	End      time.Time `json:"end"`
	Path     string    `json:"path"`
}

type EventType string

const (
	EventMotion  EventType = "motion"
	EventAI      EventType = "ai"
	EventOffline EventType = "offline"
	EventOnline  EventType = "online"
	EventManual  EventType = "manual"
)

type Event struct {
	ID         string    `json:"id"`
	DeviceID   string    `json:"deviceId"`
	DeviceName string    `json:"deviceName"`
	Type       EventType `json:"type"`
	Label      string    `json:"label"`
	Desc       string    `json:"description"`
	Time       time.Time `json:"time"`
	Snapshot   string    `json:"snapshot,omitempty"`
	GIF        string    `json:"gif,omitempty"`
	VideoStart *time.Time `json:"videoStart,omitempty"`
	VideoEnd   *time.Time `json:"videoEnd,omitempty"`
}
