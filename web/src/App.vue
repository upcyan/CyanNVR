<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { useSettingsStore } from './stores/settings'
import { useDeviceStore } from './stores/devices'
import { useNotifications } from './utils/notify'
import { http } from './api/client'

const settings = useSettingsStore()
const route = useRoute()

// 登录页与服务器设置页属于独立流程，不应出现侧边栏与底部导航
const STANDALONE_ROUTES = ['/login', '/server']
const showTabbar = computed(() => !STANDALONE_ROUTES.includes(route.path))
const showSidebar = computed(() => isDesktop.value && showTabbar.value)
const devices = useDeviceStore()
const { connect: connectSSE, disconnect: disconnectSSE } = useNotifications()
const isDesktop = ref(typeof window !== 'undefined' && window.matchMedia('(min-width: 900px)').matches)
// 侧边栏底部展示的版本号（自检用：能看到=已加载最新界面）
const appVersion = ref('')
async function loadVersion() {
  try {
    const { data } = await http.get('/api/health')
    appVersion.value = data.version || ''
  } catch { /* ignore */ }
}

let mq: MediaQueryList | null = null
let onMqChange: ((e: MediaQueryListEvent) => void) | null = null

function applyA11y() {
  const s = settings.settings
  const fontScale = { normal: '1', large: '1.2', xlarge: '1.4' }
  // 只放大文字，保持布局视口、导航断点与弹窗定位一致。
  document.documentElement.style.setProperty('--nvr-font-scale', s.careMode ? '1' : fontScale[s.fontSize] || '1')
  document.documentElement.dataset.fontSize = s.careMode ? 'normal' : s.fontSize
  document.body.classList.toggle('care', s.careMode)
  document.body.classList.toggle('light', s.theme === 'light')
  document.body.classList.toggle('dark', s.theme === 'dark')
}

onMounted(async () => {
  // 关键时序：登录后首次进入受保护路由时，pinia auth store 可能还没把
  // nvr_token 写进 localStorage（登录动作与 App 挂载存在竞争）。此时
  // devices.load() 会发出不带 Authorization 的 /api/devices → 401 →
  // 拦截器清空 token 并跳回登录页，表现为「登录成功却看到暂无设备」。
  // 这里等一小会儿直到 token 就位再加载设备，避免首屏 401。
  for (let i = 0; i < 40 && !localStorage.getItem('nvr_token'); i++) {
    await new Promise((r) => setTimeout(r, 50))
  }
  devices.load().then(() => devices.startPolling())
  mq = window.matchMedia('(min-width: 900px)')
  isDesktop.value = mq.matches
  onMqChange = (e) => (isDesktop.value = e.matches)
  mq.addEventListener('change', onMqChange)
  applyA11y()
  connectSSE()
  loadVersion()
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
      <aside v-if="showSidebar" class="sidebar">
        <div class="brand">
          <svg viewBox="0 0 100 100" class="logo">
            <rect width="100" height="100" rx="22" fill="#171a21" />
            <g fill="none" stroke="#2ea8ff" stroke-width="6" stroke-linecap="round" stroke-linejoin="round">
              <rect x="16" y="32" width="50" height="38" rx="6" />
              <path d="M66 45l16-9v28l-16-9" />
            </g>
            <circle cx="41" cy="51" r="9" fill="#2ea8ff" />
          </svg>
          <div class="brand-text">
            <span class="brand-name">CyanNVR</span>
            <span class="brand-sub">网络视频录像机</span>
          </div>
        </div>

        <div class="side-status" :class="{ warn: devices.devices.length > 0 && devices.onlineCount === 0 }">
          <span class="dot" />
          <div class="status-text">
            <b>{{ devices.onlineCount }} / {{ devices.devices.length }}</b>
            <span>摄像头在线</span>
          </div>
        </div>

        <div class="nav-group">导航</div>
        <nav class="nav">
          <router-link to="/live" class="nav-item" active-class="on">
            <span class="nav-ico"><van-icon name="play-circle-o" size="18" /></span>
            <span>摄像机</span>
            <span v-if="devices.onlineCount" class="nav-badge ok">{{ devices.onlineCount }}</span>
          </router-link>
          <router-link to="/playback" class="nav-item" active-class="on">
            <span class="nav-ico"><van-icon name="video-o" size="18" /></span>
            <span>录像管理</span>
          </router-link>
          <router-link to="/events" class="nav-item" active-class="on">
            <span class="nav-ico"><van-icon name="bell" size="18" /></span>
            <span>事件中心</span>
          </router-link>
        </nav>

        <div class="nav-group">系统</div>
        <nav class="nav">
          <router-link to="/settings" class="nav-item" active-class="on">
            <span class="nav-ico"><van-icon name="setting-o" size="18" /></span>
            <span>设置</span>
          </router-link>
        </nav>

        <div class="side-foot">
          <span class="dot" :class="{ off: devices.devices.length > 0 && devices.onlineCount === 0 }" />
          <div class="foot-text">
            <span>{{ devices.onlineCount === devices.devices.length && devices.devices.length > 0 ? '全部在线' : `在线 ${devices.onlineCount}/${devices.devices.length}` }}</span>
            <span class="ver">v{{ appVersion || '—' }}</span>
          </div>
        </div>
      </aside>

      <div class="main">
        <router-view v-slot="{ Component }">
          <keep-alive :max="4">
            <component :is="Component" />
          </keep-alive>
        </router-view>
      </div>

        <van-tabbar v-if="!isDesktop && showTabbar" route fixed placeholder safe-area-inset-bottom>
          <van-tabbar-item replace to="/live" icon="play-circle-o">摄像机</van-tabbar-item>
          <van-tabbar-item replace to="/playback" icon="video-o">录像</van-tabbar-item>
          <van-tabbar-item replace to="/events" icon="bell">事件</van-tabbar-item>
          <van-tabbar-item replace to="/settings" icon="setting-o">设置</van-tabbar-item>
        </van-tabbar>
    </div>
  </van-config-provider>
</template>

<style>
/* 沉浸式手势条适配：安卓 WebView 不支持 env(safe-area-inset-bottom)，
   CyanNVR App 会监听系统 inset 后注入 --nvr-safe-bottom 覆盖此值 */
:root {
  --nvr-safe-bottom: env(safe-area-inset-bottom, 0px);
}
.van-tabbar {
  padding-bottom: calc(var(--nvr-safe-bottom) + 8px) !important;
}
</style>

<style scoped>
/* description 字形偏竖长，略缩字号使其视觉高度与其他导航图标一致 */
.van-tabbar-item :deep(.van-icon-description-o) {
  font-size: calc(20px * var(--nvr-font-scale, 1));
}
.nav-item :deep(.van-icon-description-o) {
  font-size: calc(16px * var(--nvr-font-scale, 1));
}
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
  gap: 11px;
  padding: 20px 18px 14px;
}
.brand-text {
  display: flex;
  flex-direction: column;
  line-height: 1.15;
}
.brand-name {
  font-size: calc(17px * var(--nvr-font-scale, 1));
  font-weight: 700;
  letter-spacing: .3px;
}
.brand-sub {
  font-size: calc(10px * var(--nvr-font-scale, 1));
  color: var(--nvr-text-2);
  margin-top: 2px;
}
.logo {
  width: 32px;
  height: 32px;
  border-radius: 9px;
  box-shadow: 0 2px 8px rgba(46, 168, 255, .25);
}
/* 顶部在线状态卡片 */
.side-status {
  display: flex;
  align-items: center;
  gap: 10px;
  margin: 4px 14px 6px;
  padding: 10px 12px;
  border-radius: 10px;
  background: var(--nvr-panel-2);
  border: 1px solid var(--nvr-border);
}
.side-status .dot {
  width: 9px;
  height: 9px;
  border-radius: 50%;
  background: var(--nvr-green);
  box-shadow: 0 0 6px var(--nvr-green);
  flex-shrink: 0;
}
.side-status.warn .dot {
  background: #ff4d4f;
  box-shadow: 0 0 6px #ff4d4f;
}
.status-text {
  display: flex;
  flex-direction: column;
  line-height: 1.2;
}
.status-text b {
  font-size: calc(15px * var(--nvr-font-scale, 1));
}
.status-text span {
  font-size: calc(11px * var(--nvr-font-scale, 1));
  color: var(--nvr-text-2);
}
/* 分组标签 */
.nav-group {
  padding: 12px 20px 4px;
  font-size: calc(11px * var(--nvr-font-scale, 1));
  color: var(--nvr-text-2);
  opacity: .7;
  letter-spacing: 1px;
}
.nav {
  display: flex;
  flex-direction: column;
  gap: 4px;
  padding: 2px 10px;
}
.nav:first-of-type {
  flex: 0;
}
.nav-item {
  display: flex;
  align-items: center;
  gap: 11px;
  padding: 10px 12px;
  border-radius: 10px;
  color: var(--nvr-text-2);
  text-decoration: none;
  font-size: calc(14px * var(--nvr-font-scale, 1));
  position: relative;
  transition: background 0.15s, color 0.15s;
}
.nav-item:hover {
  background: var(--nvr-panel-2);
  color: var(--nvr-text);
}
/* 图标底色块 */
.nav-ico {
  width: 30px;
  height: 30px;
  border-radius: 8px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  background: var(--nvr-panel-2);
  border: 1px solid var(--nvr-border);
  transition: background .15s, border-color .15s;
}
.nav-item.on {
  color: var(--nvr-accent);
  background: rgba(46, 168, 255, 0.10);
}
.nav-item.on .nav-ico {
  background: rgba(46, 168, 255, 0.18);
  border-color: rgba(46, 168, 255, 0.45);
  color: var(--nvr-accent);
}
/* 活动项左侧高亮条 */
.nav-item.on::before {
  content: '';
  position: absolute;
  left: -10px;
  top: 50%;
  transform: translateY(-50%);
  width: 3px;
  height: 60%;
  border-radius: 0 3px 3px 0;
  background: var(--nvr-accent);
}
/* 在线数徽章 */
.nav-badge {
  margin-left: auto;
  min-width: 20px;
  height: 18px;
  padding: 0 6px;
  border-radius: 9px;
  font-size: calc(11px * var(--nvr-font-scale, 1));
  display: inline-flex;
  align-items: center;
  justify-content: center;
  background: var(--nvr-panel-2);
  color: var(--nvr-text-2);
}
.nav-badge.ok {
  background: rgba(46, 204, 143, .18);
  color: var(--nvr-green);
}
/* 底部状态 */
.side-foot {
  margin-top: auto;
  padding: 12px 18px;
  font-size: calc(12px * var(--nvr-font-scale, 1));
  color: var(--nvr-text-2);
  border-top: 1px solid var(--nvr-border);
  display: flex;
  align-items: center;
  gap: 8px;
}
.side-foot .dot {
  width: 7px;
  height: 7px;
  border-radius: 50%;
  background: var(--nvr-green);
  flex-shrink: 0;
}
.side-foot .dot.off {
  background: #ff4d4f;
}
.foot-text {
  display: flex;
  flex-direction: column;
  line-height: 1.25;
}
.foot-text .ver {
  font-size: calc(10px * var(--nvr-font-scale, 1));
  opacity: .65;
}
</style>
