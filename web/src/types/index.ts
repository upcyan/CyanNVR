export interface Stream {
  id: string
  name: string
  url: string
}

export interface Device {
  id: string
  name: string
  ip: string
  port: number
  username: string
  password?: string
  online: boolean
  source: 'rtsp' | 'test'
  model?: string
  rtspUrl?: string
  created?: string
  recordEnabled?: boolean
  recordMode?: RecordMode
  scheduleStart?: string
  scheduleEnd?: string
  aiEnabled?: boolean
  streams?: Stream[]
  previewStream?: string
  recordStream?: string
}

export interface DiscoveredDevice {
  ip: string
  port: number
  name: string
  xaddr?: string
  /** 归一化厂商标识（hikvision/dahua/xiaomi/tplink/unknown 等），决定列表里显示哪个 logo */
  vendor?: string
  /** 厂商展示名，例如 "TP-LINK" */
  manufacturer?: string
  /** 厂商判定依据：onvif（设备自报）/ oui（MAC 查表）/ xiaomi（端口特征）/ none */
  vendorSource?: string
  /** 设备型号，来自 ONVIF scope hardware */
  hardware?: string
  /** 设备位置，来自 ONVIF scope location */
  location?: string
  /** 设备网卡 MAC，仅同网段时填充 */
  mac?: string
  /** 探测到的直连拉流地址（小米摄像头会带上） */
  rtspUrl?: string
}

export interface RecordingSegment {
  id: string
  deviceId: string
  start: number
  end: number
  path: string
}

export interface DayRecord {
  date: string
  hasRecording: boolean
  duration: number
  segments: number
}

export type RecordMode = 'continuous' | 'motion' | 'schedule'

export type FontSize = 'normal' | 'large' | 'xlarge'

export interface AppEvent {
  id: string
  deviceName: string
  type: 'motion' | 'offline' | 'online'
  text: string
  time: number
}

export interface EventItem {
  id: string
  deviceId: string
  deviceName: string
  type: 'motion' | 'offline' | 'online' | 'ai' | 'manual'
  label?: string
  description?: string
  time: number
  snapshot?: string
  gif?: string
  videoStart?: number
  videoEnd?: number
}

export interface AIConfig {
  enabled: boolean
  mode: 'local' | 'openai'
  baseUrl: string
  detectUrl: string
  model: string
  modelPath?: string
  apiKey: string
  prompt: string
  interval: number
  cooldown: number
  threshold: number
}

export interface Settings {
  retentionDays: number
  recordMode: RecordMode
  scheduleStart: string
  scheduleEnd: string
  motionPush: boolean
  offlinePush: boolean
  https: boolean
  httpsPort: number
  tlsCertMode: string
  tlsDomain: string
  acmeEmail: string
  ai: AIConfig
  theme: 'dark' | 'light'
  fontSize: FontSize
  careMode: boolean
  demoMode: boolean
}
