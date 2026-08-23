import { http } from './client'
import { server } from './server'
import * as mock from './mock'
import type { DayRecord, Device, DiscoveredDevice, EventItem, RecordingSegment, Stream } from '../types'

export let backendOk = false
const DEMO_KEY = 'nvr_demo_mode'

export function isDemoMode(): boolean {
  try {
    return localStorage.getItem(DEMO_KEY) === '1'
  } catch {
    return false
  }
}

export function setDemoMode(on: boolean): void {
  try {
    localStorage.setItem(DEMO_KEY, on ? '1' : '0')
  } catch {
    /* ignore */
  }
}

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
  if (isDemoMode()) return mock.fetchDevices()
  if (!backendOk) return []
  const { data } = await http.get('/api/devices')
  return (data.devices ?? []) as Device[]
}

export async function addDevice(input: Partial<Device>): Promise<Device> {
  if (isDemoMode()) return mock.addDevice(input)
  if (!backendOk) return mock.addDevice(input)
  const { data } = await http.post('/api/devices', input)
  return data.device as Device
}

export async function removeDevice(id: string): Promise<void> {
  if (isDemoMode() || !backendOk) return mock.removeDevice(id)
  await http.delete(`/api/devices/${id}`)
}

export async function updateDevice(id: string, input: Partial<Device>): Promise<Device> {
  if (isDemoMode()) return mock.updateDevice(id, input)
  const { data } = await http.put(`/api/devices/${id}`, input)
  return data.device as Device
}

export async function testDevice(input: { ip: string; port: number; username?: string; password?: string; rtspUrl?: string }): Promise<{ ok: boolean; url?: string; error?: string }> {
  const { data } = await http.post('/api/devices/test', input)
  return data
}

export async function probeStreams(input: { ip: string; port: number; username?: string; password?: string }): Promise<Stream[]> {
  const { data } = await http.post('/api/devices/streams', input)
  return (data.streams ?? []) as Stream[]
}

export async function discoverDevices(): Promise<DiscoveredDevice[]> {
  if (isDemoMode() || !backendOk) return mock.discoverDevices()
  const { data } = await http.post('/api/devices/discover', {})
  return data.devices as DiscoveredDevice[]
}

export function liveStreamUrl(id: string): string {
  const token = localStorage.getItem('nvr_token')
  return `${server.base}/api/stream/live/${id}/index.m3u8${token ? `?token=${token}` : ''}`
}

// ---- storage ----

export interface StorageInfo {
  totalGB: number
  usedGB: number
}

export async function fetchStorageInfo(): Promise<StorageInfo> {
  const { data } = await http.get('/api/storage')
  return { totalGB: data.totalGB ?? 0, usedGB: data.usedGB ?? 0 }
}

// ---- recordings ----

export async function fetchMonthRecords(deviceId: string, ym: string): Promise<DayRecord[]> {
  if (isDemoMode()) return mock.fetchMonthRecords(deviceId, ym)
  if (!backendOk) return []
  const { data } = await http.get(`/api/devices/${deviceId}/month`, { params: { ym } })
  return data.days as DayRecord[]
}

export async function fetchDaySegments(deviceId: string, date: string): Promise<RecordingSegment[]> {
  if (isDemoMode()) return mock.fetchDaySegments(deviceId, date)
  if (!backendOk) return []
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

export async function fetchEvents(deviceId = '', date = '', type = '', offset = 0, limit = 50): Promise<{ events: EventItem[]; total: number }> {
  if (isDemoMode()) {
    const devs = await mock.fetchDevices()
    const all = mock.recentEvents(devs).map((e) => ({
      id: e.id,
      deviceId: '',
      deviceName: e.deviceName,
      type: e.type,
      label: e.type === 'motion' ? '移动侦测' : e.type === 'offline' ? '离线' : '上线',
      description: e.text,
      time: e.time,
    }))
    const filtered = type ? all.filter((e) => e.type === type) : all
    return { events: filtered.slice(offset, offset + limit), total: filtered.length }
  }
  if (!backendOk) return { events: [], total: 0 }
  const { data } = await http.get('/api/events', { params: { deviceId, date, type, offset, limit } })
  return { events: data.events as EventItem[], total: Number(data.total ?? 0) }
}

export async function deleteEvent(id: string): Promise<void> {
  if (isDemoMode() || !backendOk) return mock.deleteEvent(id)
  await http.delete(`/api/events/${id}`)
}

export function downloadRecordingURL(deviceId: string, date: string, time: string): string {
  const token = localStorage.getItem('nvr_token') || ''
  return `/api/devices/${deviceId}/recordings/${date}/${time}/download?token=${token}`
}

export function downloadEventSnapshotURL(eventId: string): string {
  const token = localStorage.getItem('nvr_token') || ''
  return `/api/events/${eventId}/snapshot/download?token=${token}`
}

export function downloadEventGIFURL(eventId: string): string {
  const token = localStorage.getItem('nvr_token') || ''
  return `/api/events/${eventId}/gif/download?token=${token}`
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
    mode: string
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

// ---- user management ----

export interface ManagedUser {
  id: string
  username: string
  role: 'admin' | 'operator' | 'user' | 'viewer'
  createdAt: string
}

export async function fetchUsers(): Promise<ManagedUser[]> {
  const { data } = await http.get('/api/users')
  return data.users as ManagedUser[]
}

export async function createUser(username: string, password: string, role: string): Promise<ManagedUser> {
  const { data } = await http.post('/api/users', { username, password, role })
  return data.user as ManagedUser
}

export async function updateUser(id: string, patch: { password?: string; role?: string }): Promise<void> {
  await http.put(`/api/users/${id}`, patch)
}

export async function deleteUser(id: string): Promise<void> {
  await http.delete(`/api/users/${id}`)
}
