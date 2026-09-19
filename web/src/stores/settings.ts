import { defineStore } from 'pinia'
import type { Settings } from '../types'
import {
  fetchAppSettings,
  fetchStorageInfo,
  isBackend,
  isDemoMode,
  saveAppSettings,
  setDemoMode,
  type AppSettings,
} from '../api'
import { defaultSettings } from '../mocks/generator'

const SK = 'nvr_settings_local'

interface LocalSettings {
  theme: 'dark' | 'light'
  fontSize: 'normal' | 'large' | 'xlarge'
  careMode: boolean
  demoMode: boolean
}

function loadLocal(): LocalSettings {
  const def: LocalSettings = { theme: 'dark', fontSize: 'normal', careMode: false, demoMode: false }
  try {
    const raw = localStorage.getItem(SK)
    if (raw) return { ...def, ...JSON.parse(raw) }
  } catch {
    /* ignore */
  }
  return def
}

function saveLocal(s: LocalSettings) {
  localStorage.setItem(SK, JSON.stringify(s))
}

function backendToSettings(b: AppSettings, local: LocalSettings): Settings {
  return {
    retentionDays: b.retentionDays ?? 30,
    recordMode: (b.recordMode as Settings['recordMode']) ?? 'continuous',
    scheduleStart: b.scheduleStart ?? '08:00',
    scheduleEnd: b.scheduleEnd ?? '20:00',
    motionPush: b.motionPush ?? true,
    offlinePush: b.offlinePush ?? true,
    https: b.https ?? false,
    ai: (b.ai ?? defaultSettings().ai) as Settings['ai'],
    ...local,
  }
}

function settingsToBackend(s: Settings): AppSettings {
  return {
    retentionDays: s.retentionDays,
    recordMode: s.recordMode,
    scheduleStart: s.scheduleStart,
    scheduleEnd: s.scheduleEnd,
    motionPush: s.motionPush,
    offlinePush: s.offlinePush,
    https: s.https,
    ai: s.ai,
  }
}

export const useSettingsStore = defineStore('settings', {
  state: () => ({
    settings: { ...defaultSettings(), ...loadLocal() } as Settings,
    storage: { totalGB: 0, usedGB: 0 },
    saving: false,
    loaded: false,
  }),
  getters: {
    theme: (s) => s.settings.theme,
  },
  actions: {
    set(patch: Partial<Settings>) {
      // 原地合并，保持 settings 对象引用不变。
      // 若整对象替换，组件中 `const s = store.settings` 持有的旧引用会失效，
      // 导致开关等控件的显示与实际值不一致。
      Object.assign(this.settings, patch)
      const local: LocalSettings = {
        theme: this.settings.theme,
        fontSize: this.settings.fontSize,
        careMode: this.settings.careMode,
        demoMode: this.settings.demoMode,
      }
      saveLocal(local)
      setDemoMode(this.settings.demoMode)
      if (isBackend()) {
        saveAppSettings(settingsToBackend(this.settings)).catch(() => {})
      }
    },
    async loadFromServer() {
      if (!isBackend() || isDemoMode()) return
      try {
        const [remote, sto] = await Promise.all([fetchAppSettings(), fetchStorageInfo()])
        const local = loadLocal()
        Object.assign(this.settings, backendToSettings(remote, local))
        this.storage = sto
        this.loaded = true
      } catch {
        /* ignore */
      }
    },
    async save() {
      this.saving = true
      const local: LocalSettings = {
        theme: this.settings.theme,
        fontSize: this.settings.fontSize,
        careMode: this.settings.careMode,
        demoMode: this.settings.demoMode,
      }
      saveLocal(local)
      setDemoMode(this.settings.demoMode)
      if (isBackend()) {
        try {
          const saved = await saveAppSettings(settingsToBackend(this.settings))
          Object.assign(this.settings, backendToSettings(saved, local))
        } catch {
          /* ignore */
        }
      }
      this.saving = false
    },
  },
})
