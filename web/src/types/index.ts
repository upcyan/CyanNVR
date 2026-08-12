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
  ai: AIConfig
  theme: 'dark' | 'light'
  fontSize: FontSize
  careMode: boolean
  demoMode: boolean
}
