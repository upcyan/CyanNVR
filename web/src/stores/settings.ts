import { defineStore } from 'pinia'
import { showToast } from 'vant'
import type { Settings } from '../types'
import { fetchAppSettings, fetchStorageInfo, isBackend, isDemoMode, patchAppSettings, setDemoMode, type AppSettings, type AppSettingsPatch } from '../api'
import { defaultSettings } from '../mocks/generator'

const SK = 'nvr_settings_local'
interface LocalSettings { theme: 'dark' | 'light'; fontSize: 'normal' | 'large' | 'xlarge'; careMode: boolean; demoMode: boolean }
const LOCAL_KEYS = ['theme', 'fontSize', 'careMode', 'demoMode'] as const
function loadLocal(): LocalSettings {
  const result: LocalSettings = { theme: 'dark', fontSize: 'normal', careMode: false, demoMode: false }
  try {
    const raw = JSON.parse(localStorage.getItem(SK) || '{}')
    if (raw?.theme === 'dark' || raw?.theme === 'light') result.theme = raw.theme
    if (['normal', 'large', 'xlarge'].includes(raw?.fontSize)) result.fontSize = raw.fontSize
    if (typeof raw?.careMode === 'boolean') result.careMode = raw.careMode
    if (typeof raw?.demoMode === 'boolean') result.demoMode = raw.demoMode
    // Migrate old whole-settings objects; never spread arbitrary local keys into server settings.
    localStorage.setItem(SK, JSON.stringify(result))
  } catch { /* invalid JSON/storage disabled: use display defaults only */ }
  return result
}
function localOf(s: Settings): LocalSettings { return { theme: s.theme, fontSize: s.fontSize, careMode: s.careMode, demoMode: s.demoMode } }
function saveLocal(s: LocalSettings) { try { localStorage.setItem(SK, JSON.stringify(s)) } catch { /* private mode */ } }
function fromBackend(remote: AppSettings, local: LocalSettings): Settings {
  return { ...defaultSettings(), ...remote, ai: { ...defaultSettings().ai, ...remote.ai }, ...local } as Settings
}
function toBackend(s: Settings): AppSettings {
  return { retentionDays: s.retentionDays, retentionSizeGB: s.retentionSizeGB, recordMode: s.recordMode,
    scheduleStart: s.scheduleStart, scheduleEnd: s.scheduleEnd, motionPush: s.motionPush, offlinePush: s.offlinePush,
    https: s.https, httpsPort: s.httpsPort, tlsCertMode: s.tlsCertMode, tlsDomain: s.tlsDomain,
    acmeEmail: s.acmeEmail, ai: s.ai, trustWindowHours: s.trustWindowHours ?? 0 }
}
const clone = <T>(value: T): T => JSON.parse(JSON.stringify(value))
/** Compute changed leaf fields, not the whole stale AI object. */
function difference(base: AppSettings, next: AppSettings): AppSettingsPatch {
  const patch: Record<string, unknown> = {}
  for (const key of Object.keys(next) as (keyof AppSettings)[]) {
    if (key === 'ai') {
      const ai: Record<string, unknown> = {}
      for (const field of Object.keys(next.ai) as (keyof AppSettings['ai'])[]) {
        if (next.ai[field] !== base.ai[field]) ai[field] = next.ai[field]
      }
      if (Object.keys(ai).length) patch.ai = ai
    } else if (next[key] !== base[key]) patch[key] = next[key]
  }
  return patch as AppSettingsPatch
}
let epoch = 0
let savingFlight: Promise<void> | null = null
let loadingFlight: Promise<void> | null = null

export const useSettingsStore = defineStore('settings', {
  state: () => ({ settings: { ...defaultSettings(), ...loadLocal() } as Settings,
    storage: { totalGB: 0, usedGB: 0 }, saving: false, loading: false, loaded: false,
    error: '', storageError: '', baseline: null as AppSettings | null }),
  getters: { theme: s => s.settings.theme },
  actions: {
    reset() {
      epoch++
      savingFlight = null
      loadingFlight = null
      // Keep the object reference used by SettingsView; remove identity-bound values.
      Object.assign(this.settings, { ...defaultSettings(), ...loadLocal() })
      this.loaded = false; this.loading = false; this.saving = false; this.baseline = null
      this.error = ''; this.storageError = ''; this.storage = { totalGB: 0, usedGB: 0 }
    },
    set(patch: Partial<Settings>, autosave = true) {
      const serverEdit = Object.keys(patch).some(key => !(LOCAL_KEYS as readonly string[]).includes(key))
      if (serverEdit && isBackend() && !isDemoMode() && (!this.loaded || this.loading || !!this.error)) {
        showToast('设置尚未读取成功，请先重试读取')
        return
      }
      Object.assign(this.settings, patch)
      saveLocal(localOf(this.settings)); setDemoMode(this.settings.demoMode)
      if (autosave && serverEdit && isBackend() && !isDemoMode()) {
        void this.flush().catch(() => showToast('设置保存失败，请重试'))
      }
    },
    flush(): Promise<void> {
      if (!isBackend() || isDemoMode()) return Promise.resolve()
      if (!this.loaded || !this.baseline || this.loading || this.error) return Promise.reject(new Error('设置正在读取或读取失败，请重试后保存'))
      if (savingFlight) return savingFlight
      const generation = epoch
      this.saving = true
      const flight = (async () => {
        while (generation === epoch && this.baseline) {
          const sent = clone(toBackend(this.settings))
          const patch = difference(this.baseline, sent)
          if (!Object.keys(patch).length) return
          const saved = await patchAppSettings(patch)
          if (generation !== epoch) return
          // Preserve edits made while this request was in flight; next iteration saves them.
          const later = difference(sent, toBackend(this.settings))
          Object.assign(this.settings, fromBackend(saved, localOf(this.settings)))
          if (later.ai) Object.assign(this.settings.ai, later.ai)
          const { ai: _ai, ...rest } = later
          Object.assign(this.settings, rest)
          this.baseline = clone(saved)
          this.error = ''
        }
      })()
      savingFlight = flight
      void flight.finally(() => {
        if (generation === epoch) { this.saving = false; savingFlight = null }
      }).catch(() => {})
      return flight
    },
    loadFromServer(): Promise<void> {
      if (!isBackend() || isDemoMode()) return Promise.resolve()
      if (loadingFlight) return loadingFlight
      const generation = epoch
      this.loading = true; this.error = ''
      const flight = (async () => {
        // Do not discard unsaved edits on re-entry or visibility refresh.
        if (savingFlight) await savingFlight
        const remote = await fetchAppSettings()
        if (generation !== epoch) return
        const dirty = this.baseline ? difference(this.baseline, toBackend(this.settings)) : {}
        Object.assign(this.settings, fromBackend(remote, localOf(this.settings)))
        if (dirty.ai) Object.assign(this.settings.ai, dirty.ai)
        const { ai: _ai, ...rest } = dirty
        Object.assign(this.settings, rest)
        this.baseline = clone(remote); this.loaded = true; this.loading = false
        // Storage status is independent: a disk query failure must not mask AI settings.
        try {
          const storage = await fetchStorageInfo()
          if (generation === epoch) { this.storage = storage; this.storageError = '' }
        } catch {
          if (generation === epoch) this.storageError = '磁盘信息读取失败'
        }
      })().catch((err: any) => {
        if (generation === epoch) this.error = err?.response?.data?.error || err?.message || '设置读取失败'
      })
      loadingFlight = flight
      void flight.finally(() => {
        if (generation === epoch) { this.loading = false; loadingFlight = null }
      })
      return flight
    },
    async save() {
      saveLocal(localOf(this.settings)); setDemoMode(this.settings.demoMode)
      await this.flush()
    },
  },
})
