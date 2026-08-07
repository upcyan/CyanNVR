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
	Password string       `json:"password"`
	Source   DeviceSource `json:"source"`
	Model    string       `json:"model"`
	Online   bool         `json:"online"`
	RTSPURL  string       `json:"rtspUrl,omitempty"`
	Created  time.Time    `json:"created"`
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
