import type { DayRecord, Device, DiscoveredDevice, RecordingSegment, Settings } from '../types'
import {
  daySegments,
  defaultSettings,
  discoveryCandidates,
  initialDevices,
  makeDevice,
  monthRecords,
} from '../mocks/generator'

const DEV_KEY = 'nvr_devices_v1'

const delay = (ms: number) => new Promise<void>((r) => setTimeout(r, ms))

function loadDevices(): Device[] {
  try {
    const raw = localStorage.getItem(DEV_KEY)
    if (raw) {
      const list = JSON.parse(raw) as Device[]
      if (Array.isArray(list) && list.length) return list
    }
  } catch {
    /* ignore */
  }
  return initialDevices()
}

function persistDevices(list: Device[]) {
  localStorage.setItem(DEV_KEY, JSON.stringify(list))
}

export async function fetchDevices(): Promise<Device[]> {
  await delay(300)
  return loadDevices()
}

export async function addDevice(input: Partial<Device>): Promise<Device> {
  await delay(500)
  const d = makeDevice(input)
  const list = loadDevices()
  list.push(d)
  persistDevices(list)
  return d
}

export async function removeDevice(id: string): Promise<void> {
  await delay(300)
  persistDevices(loadDevices().filter((d) => d.id !== id))
}

export async function updateDevice(id: string, input: Partial<Device>): Promise<Device> {
  await delay(300)
  const list = loadDevices()
  const idx = list.findIndex((d) => d.id === id)
  if (idx >= 0) {
    list[idx] = { ...list[idx], ...input }
    persistDevices(list)
    return list[idx]
  }
  throw new Error('device not found')
}

export async function discoverDevices(): Promise<DiscoveredDevice[]> {
  await delay(1200)
  return discoveryCandidates().map((d) => ({
    ip: d.ip,
    port: d.port,
    name: d.name,
    xaddr: `http://${d.ip}:${d.port}/onvif/device_service`,
  }))
}

export async function fetchMonthRecords(deviceId: string, ym: string): Promise<DayRecord[]> {
  await delay(250)
  return monthRecords(deviceId, ym)
}

export async function fetchDaySegments(deviceId: string, date: string): Promise<RecordingSegment[]> {
  await delay(200)
  return daySegments(deviceId, date)
}

export async function fetchSettings(): Promise<Settings> {
  await delay(150)
  return defaultSettings()
}

export async function saveSettings(s: Settings): Promise<Settings> {
  await delay(200)
  return { ...s }
}

export async function deleteEvent(_id: string): Promise<void> {
  await delay(200)
}

export { recentEvents } from '../mocks/generator'
