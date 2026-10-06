import { http } from './client'
import { server } from './server'
import * as mock from './mock'
import type { DayRecord, Device, DiscoveredDevice, EventItem, RecordingSegment, Stream } from '../types'

// 连接状态 / 演示模式统一由 conn 模块管理（两者不再互相推导）
export {
  connState,
  connError,
  demoMode,
  isBackend,
  isConnChecking,
  isDemoMode,
  setDemoMode,
  refreshConnection,
  requireBackend,
  ConnUnavailableError,
} from './conn'
import { isBackend, isDemoMode, refreshConnection, requireBackend } from './conn'

export function apiBase() {
  return server.base
}

/**
 * 启动时的连接探测。
 * 返回是否连上后端；无论结果如何都**不**改变演示模式——
 * 演示模式只由用户显式开关决定（见 conn.ts）。
 */
export async function initApi(): Promise<boolean> {
  return refreshConnection()
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

// ---- devices ----

export async function fetchDevices(): Promise<Device[]> {
  if (isDemoMode()) return mock.fetchDevices()
  // 离线时抛错而不是返回 []：返回空数组会让「服务器不可达」伪装成「暂无设备」
  requireBackend()
  const { data } = await http.get('/api/devices')
  if (!Array.isArray(data?.devices)) throw new Error('设备列表响应格式异常，请检查服务器地址')
  return data.devices as Device[]
}

export async function addDevice(input: Partial<Device>, opts?: { force?: boolean }): Promise<Device> {
  if (isDemoMode()) return mock.addDevice(input)
  requireBackend()
  const { data } = await http.post('/api/devices', input, {
    params: opts?.force ? { force: 'true' } : undefined,
  })
  return data.device as Device
}

export async function removeDevice(id: string): Promise<void> {
  if (isDemoMode()) return mock.removeDevice(id)
  requireBackend()
  await http.delete(`/api/devices/${id}`)
}

export async function updateDevice(id: string, input: Partial<Device>, opts?: { force?: boolean }): Promise<Device> {
  if (isDemoMode()) return mock.updateDevice(id, input)
  requireBackend()
  const { data } = await http.put(`/api/devices/${id}`, input, {
    params: opts?.force ? { force: 'true' } : undefined,
  })
  return data.device as Device
}

export interface TestResult {
  ok: boolean
  url?: string
  error?: string
  /** ffmpeg 探测到的视频编码，如 h264 / hevc */
  codec?: string
  /** 分辨率，探测成功时给出 */
  width?: number
  height?: number
  /** true = 该码流是 H.265，浏览器无法直接播放 */
  h265?: boolean
  /** 非 H.264 时的引导文案（后端下发，含具体编码与修改路径） */
  advice?: string
}

export async function testDevice(input: { ip: string; port: number; username?: string; password?: string; rtspUrl?: string }): Promise<TestResult> {
  const { data } = await http.post('/api/devices/test', input)
  return data
}

export async function probeStreams(input: { ip: string; port: number; username?: string; password?: string }): Promise<Stream[]> {
  const { data } = await http.post('/api/devices/streams', input)
  return (data.streams ?? []) as Stream[]
}

// ---- 品牌 RTSP 模板（按品牌+通道号拼地址，接 NVR 通道免手算） ----

export interface BrandTemplate {
  id: string
  name: string
  needCh?: boolean
  chHint?: string
  mainPath?: string
  subPath?: string
  note?: string
}

export async function fetchBrands(): Promise<BrandTemplate[]> {
  const { data } = await http.get('/api/devices/brands')
  return (data.brands ?? []) as BrandTemplate[]
}

/** 按品牌+通道号生成主/子码流地址（auto/custom 返回空，由调用方自行处理） */
export async function buildBrandUrl(input: {
  brand: string
  ip: string
  port?: number
  username?: string
  password?: string
  channel?: number
}): Promise<{ main: string; sub: string }> {
  const { data } = await http.post('/api/devices/rtsp-url', input)
  return { main: data.main ?? '', sub: data.sub ?? '' }
}

export async function discoverDevices(): Promise<DiscoveredDevice[]> {
  if (isDemoMode()) return mock.discoverDevices()
  requireBackend()
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
  /** 剩余可用空间（GB），用于水位提示 */
  freeGB: number
}

/** 运行状态总览（设置页「运行状态」用）：资源占用 / 磁盘水位 / 进程概况 */
export interface StatusInfo {
  version: string
  serverTime: string
  disk?: {
    totalGB: number
    usedGB: number
    freeGB: number
    usedPct: number
    path: string
    minFreeMB: number
    low: boolean
  }
  process?: {
    goroutines?: number
    memMB?: number
    cpuPercent?: number
    uptimeSec?: number
  }
  devices?: { total: number; online: number; recording: number }
  ffmpeg?: { total: number; workers: number }
}

export async function fetchStatus(): Promise<StatusInfo> {
  const { data } = await http.get('/api/status')
  return data as StatusInfo
}

export async function fetchStorageInfo(): Promise<StorageInfo> {
  const { data } = await http.get('/api/storage')
  return {
    totalGB: data.totalGB ?? 0,
    usedGB: data.usedGB ?? 0,
    freeGB: data.freeGB ?? Math.max(0, (data.totalGB ?? 0) - (data.usedGB ?? 0)),
  }
}

// ---- recordings ----

export async function fetchMonthRecords(deviceId: string, ym: string): Promise<DayRecord[]> {
  if (isDemoMode()) return mock.fetchMonthRecords(deviceId, ym)
  requireBackend()
  const { data } = await http.get(`/api/devices/${deviceId}/month`, { params: { ym } })
  return data.days as DayRecord[]
}

export async function fetchDaySegments(deviceId: string, date: string): Promise<RecordingSegment[]> {
  if (isDemoMode()) return mock.fetchDaySegments(deviceId, date)
  requireBackend()
  const { data } = await http.get(`/api/devices/${deviceId}/recordings`, { params: { date } })
  // segments 对旧版后端可能是 null（Go nil slice），须兜底为空数组，
  // 否则 null.map 抛 TypeError，无录像日期会被误报成「录像加载失败」
  return ((data.segments ?? []) as Array<{ start: string | number; end: string | number; id: string; deviceId: string; path: string }>).map((s) => ({
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
  // 后端等首个 HLS 分片最长 45s（转码路径），全局 15s 超时会把「慢但正常」
  // 的回放误判为失败；此处单独放宽。
  const { data } = await http.post(`/api/devices/${deviceId}/playback`, {
    start: new Date(start).toISOString(),
    end: new Date(end).toISOString(),
  }, { timeout: 60000 })
  const token = localStorage.getItem('nvr_token')
  return `${server.base}${data.url}${token ? `?token=${token}` : ''}`
}

/** 结束回放会话：停止服务端转码进程，避免离开页面后仍在占 CPU */
export async function stopPlayback(session: string): Promise<void> {
  if (!session || isDemoMode() || !isBackend()) return
  try {
    await http.post(`/api/playback/${session}/stop`)
  } catch {
    /* 停止失败无妨：服务端有 TTL 兜底回收 */
  }
}

// ---- events ----

export async function fetchEvents(deviceId = '', date = '', type = '', offset = 0, limit = 50): Promise<{ events: EventItem[]; total: number }> {
  if (isDemoMode()) {
    const devs = await mock.fetchDevices()
    const all = mock.recentEvents(devs).map((e) => ({
      id: e.id,
      deviceId: devs.find(d => d.name === e.deviceName)?.id || '',
      deviceName: e.deviceName,
      type: e.type,
      label: e.type === 'motion' ? '移动侦测' : e.type === 'offline' ? '离线' : '上线',
      description: e.text,
      time: e.time,
    }))
    const filtered = all.filter(e => {
      const when = new Date(e.time)
      const day = `${when.getFullYear()}-${String(when.getMonth() + 1).padStart(2, '0')}-${String(when.getDate()).padStart(2, '0')}`
      return (!type || e.type === type) && (!deviceId || e.deviceId === deviceId) && (!date || day === date)
    })
    return { events: filtered.slice(offset, offset + limit), total: filtered.length }
  }
  requireBackend()
  const { data } = await http.get('/api/events', { params: { deviceId, date, type, offset, limit } })
  return { events: data.events as EventItem[], total: Number(data.total ?? 0) }
}

export async function deleteEvent(id: string): Promise<void> {
  if (isDemoMode()) return mock.deleteEvent(id)
  requireBackend()
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
  /** 用户级关怀模式：登录即进入关怀界面，设置页隐藏字体大小/关怀开关 */
  careMode?: boolean
}

/** 拉取当前登录用户（用于编辑自身关怀模式后刷新界面状态） */
export async function fetchMe(): Promise<AuthUser> {
  const { data } = await http.get('/api/auth/me')
  return data.user as AuthUser
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
  retentionSizeGB: number
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
  /** 免登录信任窗口（小时），0=关闭 */
  trustWindowHours: number
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
  if (!data?.settings || typeof data.settings.ai?.enabled !== 'boolean') throw new Error('设置响应格式异常，无法确认 AI 状态')
  return data.settings as AppSettings
}

export type AppSettingsPatch = Partial<Omit<AppSettings, 'ai'>> & { ai?: Partial<AppSettings['ai']> }

export async function patchAppSettings(patch: AppSettingsPatch): Promise<AppSettings> {
  const { data } = await http.patch('/api/settings', patch)
  return data.settings as AppSettings
}

// Legacy full replacement is conditional; a stale client cannot silently overwrite another terminal.
export async function saveAppSettings(s: AppSettings): Promise<AppSettings> {
  const current = await http.get('/api/settings')
  const { data } = await http.put('/api/settings', s, { headers: { 'If-Match': current.headers.etag } })
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
  /** 纯数字用户编号，改名不变 */
  uid: number
  username: string
  role: 'admin' | 'operator' | 'user' | 'viewer'
  createdAt: string
  /** 用户级免登录窗口（小时）；undefined=跟随全局 */
  trustWindowHours?: number | null
  /** 用户级关怀模式：该账号登录即进入关怀界面，设置页隐藏字号/关怀开关 */
  careMode?: boolean
}

export async function fetchUsers(): Promise<ManagedUser[]> {
  const { data } = await http.get('/api/users')
  return data.users as ManagedUser[]
}

export async function createUser(username: string, password: string, role: string): Promise<ManagedUser> {
  const { data } = await http.post('/api/users', { username, password, role })
  return data.user as ManagedUser
}

export async function updateUser(id: string, patch: { password?: string; role?: string; username?: string; trustWindowHours?: number | null }): Promise<void> {
  await http.put(`/api/users/${id}`, patch)
}

export async function deleteUser(id: string): Promise<void> {
  await http.delete(`/api/users/${id}`)
}
