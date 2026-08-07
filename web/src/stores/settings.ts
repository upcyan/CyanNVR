import { defineStore } from 'pinia'
import type { Settings } from '../types'
import { saveSettings } from '../api/mock'
import { defaultSettings } from '../mocks/generator'

const SK = 'nvr_settings_v1'

function load(): Settings {
  try {
    const raw = localStorage.getItem(SK)
    if (raw) return { ...defaultSettings(), ...JSON.parse(raw) }
  } catch {
    /* ignore */
  }
  return defaultSettings()
}

export const useSettingsStore = defineStore('settings', {
  state: () => ({
    settings: load(),
    saving: false,
  }),
  getters: {
    theme: (s) => s.settings.theme,
  },
  actions: {
    set(patch: Partial<Settings>) {
      this.settings = { ...this.settings, ...patch }
      localStorage.setItem(SK, JSON.stringify(this.settings))
    },
    async save() {
      this.saving = true
      this.settings = await saveSettings(this.settings)
      localStorage.setItem(SK, JSON.stringify(this.settings))
      this.saving = false
    },
  },
})
