import { http } from './client'
import { server } from './server'
import * as mock from './mock'
import type { DayRecord, Device, EventItem, RecordingSegment } from '../types'

export let backendOk = false
export async function initApi(): Promise<boolean> {
  backendOk = await detect()
  return backendOk
}
export function isBackend() {
  return backendOk
}
export function apiBase() {
  return server.base
}

async function detect(): Promise<boolean> {
  try {
    const ctl = new AbortController()
    const t = setTimeout(() => ctl.abort(), 2500)
    const res = await fetch(server.base + '/api/health', { signal: ctl.signal })
    clearTimeout(t)
    if (!res.ok) return false
    const j = await res.json()
    return j && j.name === 'SimpleNVR'
  } catch {
    return false
  }
}

function authHeader() {
  const token = localStorage.getItem('nvr_token')
  return token ? { Authorization: `Bearer ${token}` } : undefined
}

// ---- devices ----

export async function fetchDevices(): Promise<Device[]> {
  if (!backendOk) return mock.fetchDevices()
  const { data } = await http.get('/api/devices')
  return data.devices as Device[]
}

export async function addDevice(input: Partial<Device>): Promise<Device> {
  if (!backendOk) return mock.addDevice(input)
  const { data } = await http.post('/api/devices', input)
  return data.device as Device
}

export async function removeDevice(id: string): Promise<void> {
  if (!backendOk) return mock.removeDevice(id)
  await http.delete(`/api/devices/${id}`)
}

export async function discoverDevices(): Promise<Device[]> {
  if (!backendOk) return mock.discoverDevices()
  const { data } = await http.post('/api/devices/discover', {})
  return data.devices as Device[]
}

export function liveStreamUrl(id: string): string {
  const token = localStorage.getItem('nvr_token')
  return `${server.base}/api/stream/live/${id}/index.m3u8${token ? `?token=${token}` : ''}`
}

// ---- recordings ----

export async function fetchMonthRecords(deviceId: string, ym: string): Promise<DayRecord[]> {
  if (!backendOk) return mock.fetchMonthRecords(deviceId, ym)
  const { data } = await http.get(`/api/devices/${deviceId}/month`, { params: { ym } })
  return data.days as DayRecord[]
}

export async function fetchDaySegments(deviceId: string, date: string): Promise<RecordingSegment[]> {
  if (!backendOk) return mock.fetchDaySegments(deviceId, date)
  const { data } = await http.get(`/api/devices/${deviceId}/recordings`, { params: { date } })
  return data.segments as RecordingSegment[]
}

export async function createPlayback(
  deviceId: string,
  start: number,
  end: number,
): Promise<string> {
  const { data } = await http.post(`/api/devices/${deviceId}/playback`, {
    start: new Date(start).toISOString(),
    end: new Date(end).toISOString(),
  })
  const token = localStorage.getItem('nvr_token')
  return `${server.base}${data.url}${token ? `?token=${token}` : ''}`
}

// ---- events ----

export async function fetchEvents(deviceId = '', date = ''): Promise<EventItem[]> {
  if (!backendOk) {
    const devs = await mock.fetchDevices()
    return mock.recentEvents(devs).map((e) => ({
      id: e.id,
      deviceId: '',
      deviceName: e.deviceName,
      type: e.type,
      label: e.type === 'motion' ? '移动侦测' : e.type === 'offline' ? '离线' : '上线',
      description: e.text,
      time: e.time,
    }))
  }
  const { data } = await http.get('/api/events', { params: { deviceId, date } })
  return data.events as EventItem[]
}

export async function deleteEvent(id: string): Promise<void> {
  await http.delete(`/api/events/${id}`)
}

// ---- auth ----

export interface AuthUser {
  id: string
  username: string
  role: 'admin' | 'operator' | 'user' | 'viewer'
}

export async function apiLogin(username: string, password: string): Promise<{ token: string; user: AuthUser }> {
  const { data } = await http.post('/api/auth/login', { username, password })
  return data as { token: string; user: AuthUser }
}

// ---- settings ----

export interface AppSettings {
  retentionDays: number
  recordMode: string
  scheduleStart: string
  scheduleEnd: string
  motionPush: boolean
  offlinePush: boolean
  https: boolean
  ai: {
    enabled: boolean
    baseUrl: string
    model: string
    apiKey: string
    prompt: string
    interval: number
    cooldown: number
    threshold: number
  }
}

export async function fetchAppSettings(): Promise<AppSettings> {
  const { data } = await http.get('/api/settings', { headers: authHeader() })
  return data.settings as AppSettings
}

export async function saveAppSettings(s: AppSettings): Promise<AppSettings> {
  const { data } = await http.put('/api/settings', s, { headers: authHeader() })
  return data.settings as AppSettings
}

export async function changeOwnPassword(old: string, next: string): Promise<void> {
  await http.put('/api/users/me/password', { old, new: next })
}
