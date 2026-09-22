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

// ---- 媒体 URL（<img>/<video>/<a> 等浏览器原生请求）----
// 浏览器对 <img src> 这类请求不会附带 Authorization 头，因此必须把 token
// 放进 query string。后端 auth.Manager.Middleware 与 streamAuth 均支持
// ?token= 回退，与此处对应。
export function withToken(url: string): string {
  if (!url) return ''
  const token = localStorage.getItem('nvr_token')
  if (!token || token === 'demo') return url
  const sep = url.includes('?') ? '&' : '?'
  return `${url}${sep}token=${encodeURIComponent(token)}`
}

// mediaURL 供 <img src> 使用：相对路径自动补 server.base 并附上 token。
// 后端返回的 snapshot/gif 字段是形如 /api/events/<id>/snapshot 的相对路径，
// 若不附 token 会被鉴权中间件拒绝（401 missing token），缩略图无法显示。
export function mediaURL(path: string): string {
  if (!path) return ''
  const abs = path.startsWith('/') ? `${server.base}${path}` : path
  return withToken(abs)
}

async function detect(): Promise<boolean> {
  try {
    const ctl = new AbortController()
    const t = setTimeout(() => ctl.abort(), 2500)
    const res = await fetch(server.base + '/api/health', { signal: ctl.signal })
    clearTimeout(t)
    if (!res.ok) return false
    const j = await res.json()
    return j && j.name === 'CyanNVR'
  } catch {
    return false
  }
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
  // 后端返回的 start/end 是 ISO 字符串，前端需要毫秒时间戳
  return (data.segments as Array<{ start: string | number; end: string | number; id: string; deviceId: string; path: string }>).map((s) => ({
    ...s,
    start: typeof s.start === 'string' ? new Date(s.start).getTime() : s.start,
    end: typeof s.end === 'string' ? new Date(s.end).getTime() : s.end,
  })) as RecordingSegment[]
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
  return `${server.base}/api/devices/${deviceId}/recordings/${date}/${time}/download?token=${token}`
}

export function downloadEventSnapshotURL(eventId: string): string {
  const token = localStorage.getItem('nvr_token') || ''
  return `${server.base}/api/events/${eventId}/snapshot/download?token=${token}`
}

export function downloadEventGIFURL(eventId: string): string {
  const token = localStorage.getItem('nvr_token') || ''
  return `${server.base}/api/events/${eventId}/gif/download?token=${token}`
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

// ---- 忘记密码 / 重置密码 ----
// 三步：生成重置码 → 校验换 grant → 凭 grant 设新密码。
// 重置码写在服务器数据目录的 reset-code.txt，网页上不显示，
// 用户需通过 SSH / Docker exec / 文件管理器读取。

export interface ResetRequestResult {
  ok: boolean
  /** 重置码文件的绝对路径，用于在界面上提示用户去哪里查看 */
  file: string
  /** 过期时刻（RFC3339），前端据此倒计时 */
  expiresAt: string
  /** 有效期（秒） */
  ttl: number
}

export async function apiResetRequest(): Promise<ResetRequestResult> {
  const { data } = await http.post('/api/auth/reset-request', {})
  return data as ResetRequestResult
}

export interface ResetVerifyResult {
  ok: boolean
  /** 一次性临时凭证，用于设置新密码 */
  grant: string
  user: string
  expiresAt: string
  ttl: number
}

export async function apiResetVerify(code: string, username = ''): Promise<ResetVerifyResult> {
  const { data } = await http.post('/api/auth/reset-verify', { code, username })
  return data as ResetVerifyResult
}

export async function apiResetConfirm(
  grant: string,
  password: string,
): Promise<{ ok: boolean; user: string; message: string }> {
  const { data } = await http.post('/api/auth/reset-confirm', { grant, password })
  return data as { ok: boolean; user: string; message: string }
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
  httpsPort: number
  tlsCertMode: string
  tlsDomain: string
  acmeEmail: string
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

export interface TLSStatus {
  enabled: boolean
  running: boolean
  mode: string
  domain: string
  port: number
  hasManual: boolean
  notAfter?: string
  issuer?: string
  message?: string
}

export async function fetchAppSettings(): Promise<AppSettings> {
  const { data } = await http.get('/api/settings')
  return data.settings as AppSettings
}

export async function saveAppSettings(s: AppSettings): Promise<AppSettings> {
  const { data } = await http.put('/api/settings', s)
  return data.settings as AppSettings
}

export async function fetchTLSStatus(): Promise<TLSStatus> {
  const { data } = await http.get('/api/tls/status')
  return data as TLSStatus
}

export async function uploadManualCert(cert: string, key: string): Promise<TLSStatus> {
  const { data } = await http.post('/api/tls/manual-cert', { cert, key })
  return data as TLSStatus
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
