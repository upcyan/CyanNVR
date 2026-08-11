<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useSettingsStore } from './stores/settings'
import { useDeviceStore } from './stores/devices'
import { useNotifications, requestNotificationPermission } from './utils/notify'

const settings = useSettingsStore()
const devices = useDeviceStore()
const { connect: connectSSE, disconnect: disconnectSSE } = useNotifications()
const isDesktop = ref(false)

let mq: MediaQueryList | null = null
let onMqChange: ((e: MediaQueryListEvent) => void) | null = null

function applyA11y() {
  const s = settings.settings
  const zoomMap = { normal: '1', large: '1.2', xlarge: '1.4' }
  document.documentElement.style.zoom = s.careMode ? '1.4' : zoomMap[s.fontSize]
  document.body.classList.toggle('care', s.careMode)
  document.body.classList.toggle('light', s.theme === 'light')
  document.body.classList.toggle('dark', s.theme === 'dark')
}

onMounted(() => {
  devices.load().then(() => devices.startPolling())
  mq = window.matchMedia('(min-width: 900px)')
  isDesktop.value = mq.matches
  onMqChange = (e) => (isDesktop.value = e.matches)
  mq.addEventListener('change', onMqChange)
  applyA11y()
  connectSSE()
  requestNotificationPermission()
})

watch(
  () => [settings.settings.fontSize, settings.settings.careMode, settings.settings.theme],
  applyA11y,
  { immediate: true },
)

watch(
  () => settings.settings.demoMode,
  () => {
    devices.load().then(() => devices.startPolling())
  },
)

onBeforeUnmount(() => {
  devices.stopPolling()
  disconnectSSE()
  if (mq && onMqChange) mq.removeEventListener('change', onMqChange)
})
</script>

<template>
  <van-config-provider :theme="settings.settings.theme">
    <div
      class="app-shell"
      :class="[isDesktop ? 'desktop' : '']"
    >
      <aside v-if="isDesktop" class="sidebar">
        <div class="brand">
          <svg viewBox="0 0 100 100" class="logo">
            <rect width="100" height="100" rx="22" fill="#171a21" />
            <g fill="none" stroke="#2ea8ff" stroke-width="6" stroke-linecap="round" stroke-linejoin="round">
              <rect x="16" y="32" width="50" height="38" rx="6" />
              <path d="M66 45l16-9v28l-16-9" />
            </g>
            <circle cx="41" cy="51" r="9" fill="#2ea8ff" />
          </svg>
          <span>SimpleNVR</span>
        </div>
        <nav class="nav">
          <router-link to="/live" class="nav-item" active-class="on">
            <van-icon name="video-o" size="18" />
            <span>摄像机</span>
          </router-link>
          <router-link to="/playback" class="nav-item" active-class="on">
            <van-icon name="play-circle-o" size="18" />
            <span>录像管理</span>
          </router-link>
          <router-link to="/settings" class="nav-item" active-class="on">
            <van-icon name="setting-o" size="18" />
            <span>设置</span>
          </router-link>
        </nav>
        <div class="side-foot">
          <span class="dot" />
          在线 {{ devices.onlineCount }} / {{ devices.devices.length }}
        </div>
      </aside>

      <div class="main">
        <router-view v-slot="{ Component }">
          <keep-alive :max="2">
            <component :is="Component" />
          </keep-alive>
        </router-view>
      </div>

      <van-tabbar v-if="!isDesktop" route fixed placeholder safe-area-inset-bottom>
        <van-tabbar-item replace to="/live" icon="video-o">摄像机</van-tabbar-item>
        <van-tabbar-item replace to="/playback" icon="play-circle-o">录像管理</van-tabbar-item>
      </van-tabbar>
    </div>
  </van-config-provider>
</template>

<style scoped>
.sidebar {
  width: 216px;
  flex-shrink: 0;
  display: flex;
  flex-direction: column;
  background: var(--nvr-panel);
  border-right: 1px solid var(--nvr-border);
  height: 100%;
}
.brand {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 20px 18px 16px;
  font-size: 17px;
  font-weight: 700;
}
.logo {
  width: 30px;
  height: 30px;
  border-radius: 8px;
}
.nav {
  display: flex;
  flex-direction: column;
  gap: 4px;
  padding: 8px 10px;
  flex: 1;
}
.nav-item {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 11px 12px;
  border-radius: 10px;
  color: var(--nvr-text-2);
  text-decoration: none;
  font-size: 14px;
  transition: background 0.15s, color 0.15s;
}
.nav-item:hover {
  background: var(--nvr-panel-2);
  color: var(--nvr-text);
}
.nav-item.on {
  background: rgba(46, 168, 255, 0.12);
  color: var(--nvr-accent);
}
.side-foot {
  padding: 14px 18px;
  font-size: 12px;
  color: var(--nvr-text-2);
  border-top: 1px solid var(--nvr-border);
  display: flex;
  align-items: center;
  gap: 6px;
}
.side-foot .dot {
  width: 7px;
  height: 7px;
  border-radius: 50%;
  background: var(--nvr-green);
}
</style>
