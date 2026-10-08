package models

import "time"

type Role string

const (
	RoleAdmin    Role = "admin"
	RoleUser     Role = "user"
	RoleViewer   Role = "viewer"
	RoleOperator Role = "operator"
)

type User struct {
	ID string `json:"id"`
	// UID 纯数字用户编号：创建时自动分配（现有最大值+1），改名不变；
	// 仅供人类识别展示，会话/事件等身份判定仍走不可变的 id。
	UID          int       `json:"uid"`
	Username     string    `json:"username"`
	PasswordHash string    `json:"-"`
	Role         Role      `json:"role"`
	CreatedAt    time.Time `json:"createdAt"`
	// PasswordChangedAt 最后一次改密时间。早于该时刻签发的 JWT 视为失效，
	// 用于重置/修改密码后让旧会话立即下线。零值表示从未改密（不做吊销）。
	PasswordChangedAt time.Time `json:"passwordChangedAt,omitzero"`
	// TrustWindowHours 用户级免登录窗口（小时）。nil=跟随全局设置；
	// >=0 为专属覆盖（0=该用户关闭免登录）。
	TrustWindowHours *int64 `json:"trustWindowHours,omitempty"`
	// CareMode 用户级关怀模式：开启后该账号登录即进入关怀模式（更大字体与按钮），
	// 且设置页不再展示「字体大小」「关怀模式」两项（避免与用户级设置互相覆盖）。
	CareMode bool `json:"careMode"`
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

	// Per-device retention limits. RetentionDays = 0 follows the global
	// retention days; RetentionSizeGB = 0 means no per-device size cap
	// (still bounded by the global total size limit).
	RetentionDays   int `json:"retentionDays"`
	RetentionSizeGB int `json:"retentionSizeGB"`

	// Per-device AI analysis override (defaults to global AI setting when unset).
	AIEnabled *bool `json:"aiEnabled,omitempty"`

	// ConnMode 记录该设备的 RTSP 连接策略：
	//   ""/"auto" 自动（先用多连接，失败则降级）
	//   "multi"   多连接（录制/预览/快照各自独立连接）
	//   "single"  单连接（tee 单连接同时输出，适配仅允许 1 个会话的摄像头）
	// 自动降级后会把结果写回该字段，避免每次重启重复试错。
	ConnMode string `json:"connMode,omitempty"`

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

// Mark 是用户在回放时间轴上打的标记点。
//
// 用途：① 快速跳转到关注时刻 ② 导出时选起止。必须持久化，
// 否则退出页面即丢，用户无法积累关注点（此前就是内存数组）。
type Mark struct {
	ID        string    `json:"id"`
	DeviceID  string    `json:"deviceId"`
	Time      time.Time `json:"time"`
	Note      string    `json:"note"`
	CreatedAt time.Time `json:"createdAt"`
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
	ID         string     `json:"id"`
	DeviceID   string     `json:"deviceId"`
	DeviceName string     `json:"deviceName"`
	Type       EventType  `json:"type"`
	Label      string     `json:"label"`
	Desc       string     `json:"description"`
	Time       time.Time  `json:"time"`
	Snapshot   string     `json:"snapshot,omitempty"`
	GIF        string     `json:"gif,omitempty"`
	VideoStart *time.Time `json:"videoStart,omitempty"`
	VideoEnd   *time.Time `json:"videoEnd,omitempty"`
}
