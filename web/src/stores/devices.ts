import { defineStore } from 'pinia'
import type { Device } from '../types'
import { addDevice, discoverDevices, fetchDevices, isBackend, removeDevice, updateDevice } from '../api'

let pollTimer: ReturnType<typeof setInterval> | null = null
/** 设备列表请求代次：只有最新一次请求的结果可以写入 store */
let listEpoch = 0
/** 失败后的快速重试定时器 */
let retryTimer: ReturnType<typeof setTimeout> | null = null

/** 加载失败后的快速重试退避（毫秒）。首屏失败若只能等 10 秒轮询，
 *  用户会先看到一段「暂无设备」，观感上像坏了；这里尽快自愈。 */
const RETRY_DELAYS = [1200, 3000]

/** 加载状态：未加载 / 加载中 / 已就绪 / 失败。不能用 devices=[] 表达全部语义。 */
export type DevicesStatus = 'idle' | 'loading' | 'ready' | 'error'

export const useDeviceStore = defineStore('devices', {
  state: () => ({
    devices: [] as Device[],
    loading: false,
    status: 'idle' as DevicesStatus,
    /** 最近一次加载失败原因（连接故障或接口错误），供界面显示重试入口 */
    error: '',
    /** 是否已成功从服务端取过一次数据；用于区分「首屏未加载」与「真的没有设备」 */
    loaded: false,
    /** 连续失败次数：用于快速重试退避，成功后归零 */
    failCount: 0,
  }),
  getters: {
    onlineCount: (s) => s.devices.filter((d) => d.online).length,
    byId: (s) => (id: string) => s.devices.find((d) => d.id === id),
    /** 真正可以对外声明「没有设备」的时刻 */
    isEmpty: (s) => s.loaded && s.devices.length === 0,
  },
  actions: {
    /** 用服务端返回的集合整体替换本地列表（以服务端为准） */
    applyList(list: Device[]) {
      this.devices = list
      this.loaded = true
      this.status = 'ready'
      this.error = ''
      this.failCount = 0
    },
    /**
     * 全量加载设备列表。
     *
     * 关键修复：此前 refresh() 只遍历本地已有数组，本地为空（首次加载失败/
     * 服务器暂无设备）时，后续轮询即使返回了设备也永远写不进去，
     * 表现为「设备一直显示不出来」。现在一律以服务端集合为准。
     */
    async load() {
      const epoch = ++listEpoch
      this.loading = true
      if (!this.loaded) this.status = 'loading'
      this.error = ''
      try {
        const list = await fetchDevices()
        // 期间有更新的请求发出：丢弃本次结果，防止旧响应覆盖新数据
        if (epoch !== listEpoch) return
        this.applyList(Array.isArray(list) ? list : [])
        this.clearRetry()
      } catch (err: any) {
        if (epoch !== listEpoch) return
        // 失败时保留最后一次可信数据，只标记错误状态，不清空列表
        this.status = 'error'
        this.error = err?.message || '设备列表加载失败'
        this.failCount++
        console.warn('[devices] load failed:', err?.message || err)
        // 快速重试：实测新浏览器首个请求偶发耗时数百毫秒到数秒，
        // 若只依赖 10 秒轮询，首屏会有明显一段「暂无设备」。
        this.scheduleRetry()
      } finally {
        if (epoch === listEpoch) this.loading = false
      }
    },
    /** 按退避表安排一次快速重试（仅前两次失败时触发） */
    scheduleRetry() {
      this.clearRetry()
      const delay = RETRY_DELAYS[this.failCount - 1]
      if (delay === undefined) return
      retryTimer = setTimeout(() => {
        retryTimer = null
        if (this.loading) return
        void this.load()
      }, delay)
    },
    clearRetry() {
      if (retryTimer) {
        clearTimeout(retryTimer)
        retryTimer = null
      }
    },
    /**
     * 轮询刷新：与 load 共用同一实现与代次保护。
     * 成功返回空数组时会真正清空列表（设备被删除后界面同步消失），
     * 失败时保留旧数据（不因网络抖动把已在用的设备清空）。
     */
    async refresh() {
      try {
        await this.load()
      } catch {
        /* load 内部已处理错误状态 */
      }
    },
    async add(input: Partial<Device>, opts?: { force?: boolean }) {
      const d = await addDevice(input, opts)
      this.devices.push(d)
      this.loaded = true
      this.status = 'ready'
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
    startPolling() {
      this.stopPolling()
      if (!isBackend()) return
      // 用 setInterval 但内部串行：上一次未结束就跳过本次，
      // 避免 15s 超时的请求与 10s 周期叠加造成并发风暴。
      pollTimer = setInterval(() => {
        if (this.loading) return
        void this.refresh()
      }, 10000)
    },
    stopPolling() {
      if (pollTimer) {
        clearInterval(pollTimer)
        pollTimer = null
      }
      this.clearRetry()
      // 使在途请求的结果失效，避免退出登录后旧响应又写回数据
      listEpoch++
    },
    /** 重置为未加载状态（退出登录时调用），防止把上个账号的设备带给下个账号 */
    reset() {
      this.stopPolling()
      this.devices = []
      this.loading = false
      this.status = 'idle'
      this.error = ''
      this.loaded = false
      this.failCount = 0
    },
  },
})
