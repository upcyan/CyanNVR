export interface Device {
  id: string
  name: string
  ip: string
  port: number
  username: string
  password?: string
  online: boolean
  channel: number
  model?: string
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

export interface Settings {
  storageTotalGB: number
  storageUsedGB: number
  retentionDays: number
  recordMode: RecordMode
  scheduleStart: string
  scheduleEnd: string
  motionPush: boolean
  offlinePush: boolean
  httpPort: number
  httpsEnabled: boolean
  theme: 'dark' | 'light'
  fontSize: FontSize
  careMode: boolean
}
