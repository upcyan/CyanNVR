import { defineStore } from 'pinia'
import type { Device } from '../types'
import { addDevice, discoverDevices, fetchDevices, removeDevice } from '../api/mock'

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
      this.devices = await fetchDevices()
      this.loading = false
    },
    async add(input: Partial<Device>) {
      const d = await addDevice(input)
      this.devices.push(d)
      return d
    },
    async remove(id: string) {
      await removeDevice(id)
      this.devices = this.devices.filter((d) => d.id !== id)
    },
    async discover() {
      return discoverDevices()
    },
    startHeartbeat() {
      setInterval(() => {
        if (!this.devices.length) return
        const i = Math.floor(Math.random() * this.devices.length)
        this.devices[i].online = !this.devices[i].online
      }, 20000)
    },
  },
})
