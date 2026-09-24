import { defineStore } from 'pinia'
import type { Device } from '../types'
import { addDevice, discoverDevices, fetchDevices, isBackend, removeDevice, updateDevice } from '../api'

let pollTimer: ReturnType<typeof setInterval> | null = null

export const useDeviceStore = defineStore('devices', {
  state: () => ({
    devices: [] as Device[],
    loading: false,
  }),
  getters: {
    onlineCount: (s) => s.devices.filter((d) => d.online).length,
    byId: (s) => (id: string) => s.devices.find((d) => d.id === id),
  },
  actions: {
    async load() {
      this.loading = true
      try {
        const list = await fetchDevices()
        this.devices = Array.isArray(list) ? list : []
      } catch (err) {
        // 首屏偶发 401（登录竞态）不应让 Promise 变成未处理拒绝：
        // 保留已有数据，等 10 秒后的轮询自动重试即可。
        console.warn('[devices] load failed, will retry by polling:', err)
      } finally {
        this.loading = false
      }
    },
    async add(input: Partial<Device>, opts?: { force?: boolean }) {
      const d = await addDevice(input, opts)
      this.devices.push(d)
      return d
    },
    async remove(id: string) {
      await removeDevice(id)
      this.devices = this.devices.filter((d) => d.id !== id)
    },
    async update(id: string, input: Partial<Device>, opts?: { force?: boolean }) {
      const d = await updateDevice(id, input, opts)
      const idx = this.devices.findIndex((x) => x.id === id)
      if (idx >= 0) this.devices[idx] = { ...this.devices[idx], ...d }
      return d
    },
    async discover() {
      return discoverDevices()
    },
    async refresh() {
      try {
        const list = await fetchDevices()
        if (!Array.isArray(list)) return
        const byId = new Map(list.map((d) => [d.id, d]))
        this.devices = this.devices.map((d) => {
          const fresh = byId.get(d.id)
          return fresh ? { ...d, online: fresh.online, rtspUrl: fresh.rtspUrl ?? d.rtspUrl } : d
        })
      } catch {
        /* keep last known state on transient errors */
      }
    },
    startPolling() {
      this.stopPolling()
      if (!isBackend()) return
      pollTimer = setInterval(() => {
        this.refresh()
      }, 10000)
    },
    stopPolling() {
      if (pollTimer) {
        clearInterval(pollTimer)
        pollTimer = null
      }
    },
  },
})
