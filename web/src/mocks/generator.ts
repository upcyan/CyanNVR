import type { AppEvent, DayRecord, Device, RecordingSegment, Settings } from '../types'

export function hashStr(s: string): number {
  let h = 2166136261
  for (let i = 0; i < s.length; i++) {
    h ^= s.charCodeAt(i)
    h = Math.imul(h, 16777619)
  }
  return h >>> 0
}

export function mulberry32(seed: number): () => number {
  let a = seed >>> 0
  return () => {
    a |= 0
    a = (a + 0x6d2b79f5) | 0
    let t = Math.imul(a ^ (a >>> 15), 1 | a)
    t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296
  }
}

const CAMS = [
  { name: '大堂摄像头', ip: '192.168.1.101', model: 'Hikvision DS-2CD' },
  { name: '门口摄像头', ip: '192.168.1.102', model: 'Dahua IPC-HDW' },
  { name: '车库摄像头', ip: '192.168.1.103', model: 'Hikvision DS-2DE' },
  { name: '走廊摄像头', ip: '192.168.1.104', model: 'Dahua IPC-HFW' },
  { name: '后院摄像头', ip: '192.168.1.105', model: 'TP-Link IPC' },
  { name: '仓库摄像头', ip: '192.168.1.106', model: 'Uniview IPC' },
]

let uid = 0
export function makeDevice(input: Partial<Device>): Device {
  uid += 1
  return {
    id: input.id ?? `dev_${Date.now().toString(36)}_${uid}`,
    name: input.name ?? '新设备',
    ip: input.ip ?? '192.168.1.108',
    port: input.port ?? 554,
    username: input.username ?? 'admin',
    password: input.password ?? 'admin123',
    online: input.online ?? true,
    source: input.source ?? 'rtsp',
    model: input.model ?? 'ONVIF Camera',
    rtspUrl: input.rtspUrl,
    recordEnabled: input.recordEnabled ?? true,
    recordMode: input.recordMode ?? 'continuous',
    scheduleStart: input.scheduleStart ?? '08:00',
    scheduleEnd: input.scheduleEnd ?? '20:00',
    aiEnabled: input.aiEnabled,
  }
}

export function initialDevices(): Device[] {
  return CAMS.map((c, i) =>
    makeDevice({
      name: c.name,
      ip: c.ip,
      port: 554,
      username: 'admin',
      password: 'admin123',
      online: i !== 2,
      source: 'rtsp',
      model: c.model,
      rtspUrl: `rtsp://admin:admin123@${c.ip}:554/stream1`,
    }),
  )
}

export function discoveryCandidates(): Device[] {
  return CAMS.map((c, i) =>
    makeDevice({
      name: c.name,
      ip: c.ip,
      port: 554,
      username: 'admin',
      password: 'admin123',
      online: true,
      source: 'rtsp',
      model: c.model,
      rtspUrl: `rtsp://admin:admin123@${c.ip}:554/stream1`,
      id: `disc_${i}`,
    }),
  )
}

const DAY = 86400000

export function daySegments(deviceId: string, date: string): RecordingSegment[] {
  const rnd = mulberry32(hashStr(`${deviceId}:${date}`))
  const dayStart = new Date(`${date}T00:00:00`).getTime()
  const out: RecordingSegment[] = []
  let cursor = dayStart + Math.floor(rnd() * 45) * 60000
  const count = 4 + Math.floor(rnd() * 5)
  for (let i = 0; i < count; i++) {
    const start = cursor
    const dur = (8 + Math.floor(rnd() * 45)) * 60000
    const end = start + dur
    if (end > dayStart + DAY) break
    out.push({
      id: `${deviceId}_${start}`,
      deviceId,
      start,
      end,
      path: `recordings/${deviceId}/${date}/${fmtHm(start)}-${fmtHm(end)}.mp4`,
    })
    cursor = end + (15 + Math.floor(rnd() * 70)) * 60000
  }
  return out
}

export function monthRecords(deviceId: string, ym: string): DayRecord[] {
  const [y, m] = ym.split('-').map(Number)
  const dim = new Date(y, m, 0).getDate()
  const out: DayRecord[] = []
  for (let d = 1; d <= dim; d++) {
    const date = `${y}-${String(m).padStart(2, '0')}-${String(d).padStart(2, '0')}`
    const segs = daySegments(deviceId, date)
    const duration = Math.round(segs.reduce((a, s) => a + (s.end - s.start), 0) / 60000)
    out.push({
      date,
      hasRecording: segs.length > 0,
      duration,
      segments: segs.length,
    })
  }
  return out
}

function fmtHm(ts: number): string {
  const d = new Date(ts)
  return `${String(d.getHours()).padStart(2, '0')}${String(d.getMinutes()).padStart(2, '0')}`
}

export function recentEvents(devices: Device[]): AppEvent[] {
  const today = new Date()
  const dayStart = new Date(today.getFullYear(), today.getMonth(), today.getDate()).getTime()
  const elapsed = Math.max(1, Date.now() - dayStart)
  const rnd = mulberry32(hashStr(`events:${dayStart}:${devices.length}`))
  const defs: Array<{ type: AppEvent['type']; text: string }> = [
    { type: 'motion', text: '检测到移动' },
    { type: 'motion', text: '检测到移动' },
    { type: 'offline', text: '设备离线' },
    { type: 'online', text: '设备上线' },
  ]
  return devices
    .slice(0, 6)
    .flatMap((d) => {
      const n = 1 + Math.floor(rnd() * 2)
      return Array.from({ length: n }, () => {
        const def = defs[Math.floor(rnd() * defs.length)]
        const time = dayStart + Math.floor(rnd() * elapsed)
        return { id: `${d.id}_${time}`, deviceName: d.name, type: def.type, text: def.text, time }
      })
    })
    .sort((a, b) => b.time - a.time)
    .slice(0, 10)
}

export function defaultSettings(): Settings {
  return {
    retentionDays: 30,
    recordMode: 'continuous',
    scheduleStart: '08:00',
    scheduleEnd: '20:00',
    motionPush: true,
    offlinePush: true,
    https: false,
    ai: {
      enabled: false,
      baseUrl: 'https://api.openai.com/v1',
      model: 'gpt-4o-mini',
      apiKey: '',
      prompt: '你是安防监控分析助手。分析图中画面，仅输出JSON：{"alert":true/false,"label":"事件类别","description":"简短中文描述"}。出现人员、车辆、异常闯入、火焰烟雾等视为 alert=true。',
      interval: 10,
      cooldown: 60,
      threshold: 0.5,
    },
    theme: 'dark',
    fontSize: 'normal',
    careMode: false,
    demoMode: false,
  }
}
