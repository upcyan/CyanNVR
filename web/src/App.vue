<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { useSettingsStore } from './stores/settings'
import { useAuthStore } from './stores/auth'
import { useDeviceStore } from './stores/devices'
import { useNotifications } from './utils/notify'
import { http } from './api/client'
import { server } from './api/server'

const settings = useSettingsStore()
const auth = useAuthStore()
const route = useRoute()

// 登录页与服务器设置页属于独立流程，不应出现侧边栏与底部导航
const STANDALONE_ROUTES = ['/login', '/server']
const showTabbar = computed(() => !STANDALONE_ROUTES.includes(route.path))
const showSidebar = computed(() => isDesktop.value && showTabbar.value)
const devices = useDeviceStore()
const { connect: connectSSE, disconnect: disconnectSSE } = useNotifications()
const isDesktop = ref(typeof window !== 'undefined' && window.matchMedia('(min-width: 900px)').matches)
// 系统深浅色（theme='auto' 时跟随）：初始化即读取，变化由 setupColorScheme 监听
const sysDark = ref(typeof window !== 'undefined' && window.matchMedia('(prefers-color-scheme: dark)').matches)
// 实际生效的主题：'auto' 折算成系统当前值，其余为用户显式选择
const uiTheme = computed<'dark' | 'light'>(() => {
  if (settings.settings.theme === 'auto') return sysDark.value ? 'dark' : 'light'
  return settings.settings.theme === 'light' ? 'light' : 'dark'
})
// 侧边栏状态卡的在线率（0-100），有设备时展示迷你进度条
const onlineRate = computed(() =>
  devices.devices.length ? Math.round((devices.onlineCount / devices.devices.length) * 100) : 0,
)
// 侧边栏底部展示的版本号（自检用：能看到=已加载最新界面）
const appVersion = ref('')
// 状态块折叠：减少首屏视觉噪音，点击展开详情
const statusCollapsed = ref(localStorage.getItem('nvr_sidebar_collapsed') === '1')
watch(statusCollapsed, (v) => localStorage.setItem('nvr_sidebar_collapsed', v ? '1' : '0'))
async function loadVersion() {
  try {
    const { data } = await http.get('/api/health')
    appVersion.value = data.version || ''
  } catch { /* ignore */ }
}

let mq: MediaQueryList | null = null
let onMqChange: ((e: MediaQueryListEvent) => void) | null = null
let mqDark: MediaQueryList | null = null
let onMqDark: ((e: MediaQueryListEvent) => void) | null = null
// 系统深浅色变化监听：theme='auto' 时立即重算生效主题
function setupColorScheme() {
  if (mqDark) return
  mqDark = window.matchMedia('(prefers-color-scheme: dark)')
  sysDark.value = mqDark.matches
  onMqDark = (e) => {
    sysDark.value = e.matches
    applyA11y()
  }
  mqDark.addEventListener('change', onMqDark)
}
/** 已完成初始化的会话代次：避免重复初始化、也能识别「换会话」 */
let initializedEpoch = -1

function applyA11y() {
  const s = settings.settings
  // 关怀模式生效位 = 本地开关 || 用户级关怀（管理员在用户管理里为本账号勾选）。
  // 用户级关怀开启时设置页隐藏字号/关怀开关，二者不会互相覆盖。
  const careActive = s.careMode || !!auth.user?.careMode
  const fontScale = { normal: '1', large: '1.2', xlarge: '1.4' }
  // 只放大文字，保持布局视口、导航断点与弹窗定位一致。
  // 关怀模式基数取「特大字体」同档（1.4），而不是此前的 1：care 的叠加规则
  // 只覆盖部分元素，基数被钳成 1 会让未覆盖到的文字比「关闭关怀 + 特大字体」
  // 还小（实测回放页关怀 15px vs 特大 16.8px）。care 专属规则在此基础上继续
  // 放大，因此最终恒有「关怀 ≥ 特大字体」。
  document.documentElement.style.setProperty('--nvr-font-scale', careActive ? fontScale.xlarge : fontScale[s.fontSize] || '1')
  document.documentElement.dataset.fontSize = careActive ? 'normal' : s.fontSize
  document.body.classList.toggle('care', careActive)
  document.body.classList.toggle('light', uiTheme.value === 'light')
  document.body.classList.toggle('dark', uiTheme.value === 'dark')
}

/** 视口相关的初始化：与登录状态无关，任何情况下都必须执行 */
function setupViewport() {
  if (mq) return
  mq = window.matchMedia('(min-width: 900px)')
  isDesktop.value = mq.matches
  onMqChange = (e) => (isDesktop.value = e.matches)
  mq.addEventListener('change', onMqChange)
}

/** 建立会话相关资源：设备列表 + 轮询 + 实时通知 + 版本自检 */
function startSession(epoch: number) {
  settings.reset()
  void settings.loadFromServer()
  devices.load().then(() => {
    // 期间又换了会话（退出/重登）：不要为旧会话启动轮询
    if (epoch !== auth.sessionEpoch) return
    devices.startPolling()
  })
  connectSSE()
  loadVersion()
}

/** 结束会话相关资源并清空身份关联数据 */
function stopSession() {
  settings.reset()
  devices.reset()
  disconnectSSE()
}

/**
 * 会话驱动的初始化。
 *
 * 关键修复：此前只在根组件 onMounted 里「最多等 2 秒 token」，等到就初始化，
 * 等不到就 return。但用户通常是在登录页停留一会儿才登录，而登录成功只是
 * 切换路由、**不会重新挂载根组件**，于是设备加载/轮询/SSE 永远不会启动，
 * 表现为「登录成功却没有任何设备」。
 *
 * 现在改为监听会话本身：登录（token 由空变有）→ 初始化；退出 → 清理。
 * 无论用户在登录页停留多久，登录后都一定会初始化。
 */
function syncSession() {
  const epoch = auth.sessionEpoch
  const loggedIn = auth.isLoggedIn
  if (loggedIn && initializedEpoch !== epoch) {
    initializedEpoch = epoch
    startSession(epoch)
    return
  }
  if (!loggedIn && initializedEpoch !== -1) {
    initializedEpoch = -1
    stopSession()
  }
}

onMounted(() => {
  // 视口与无障碍设置与登录无关，立即初始化（此前被 token 等待分支跳过，
  // 导致登录后窗口尺寸变化不生效）
  setupViewport()
  setupColorScheme()
  applyA11y()
  // 会话可能已经存在（刷新页面/信任窗口内重开），也可能稍后才建立（登录）。
  // 两种情况都由同一个同步函数处理。
  syncSession()
  // 已登录但未跳转到受保护路由时（例如直接停在登录页），交由路由守卫处理
})

// 登录/退出/切换账号都会改变 token 或会话代次 → 自动重建或清理会话资源
watch(
  () => [auth.token, auth.sessionEpoch] as const,
  () => syncSession(),
)

watch(
  () => [settings.settings.fontSize, settings.settings.careMode, settings.settings.theme, auth.user?.careMode],
  applyA11y,
  { immediate: true },
)

watch(
  () => settings.settings.demoMode,
  () => {
    // 演示模式切换会整体换数据源：重置并重新加载
    devices.reset()
    devices.load().then(() => devices.startPolling())
  },
)

watch(() => server.base, () => {
  // A different backend may have a different user database; never reuse its session/data.
  settings.reset()
  devices.reset()
  disconnectSSE()
  auth.logout()
})

onBeforeUnmount(() => {
  devices.stopPolling()
  disconnectSSE()
  if (mq && onMqChange) mq.removeEventListener('change', onMqChange)
  if (mqDark && onMqDark) mqDark.removeEventListener('change', onMqDark)
})
</script>

<template>
  <van-config-provider :theme="uiTheme">
    <div
      class="app-shell"
      :class="[isDesktop ? 'desktop' : '']"
    >
      <aside v-if="showSidebar" class="sidebar">
        <div class="brand">
          <svg viewBox="0 0 100 100" class="logo">
            <defs>
              <linearGradient id="logo-g" x1="0" y1="0" x2="1" y2="1">
                <stop offset="0" stop-color="#20334a" />
                <stop offset="1" stop-color="#0f1420" />
              </linearGradient>
            </defs>
            <rect width="100" height="100" rx="22" fill="url(#logo-g)" stroke="rgba(46, 168, 255, 0.4)" stroke-width="2" />
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

        <div class="side-status" :class="{ warn: devices.devices.length > 0 && devices.onlineCount === 0, collapsed: statusCollapsed }" role="button" tabindex="0" :aria-expanded="!statusCollapsed" aria-label="摄像头在线状态，点击折叠或展开" @keydown.enter.prevent="statusCollapsed = !statusCollapsed" @keydown.space.prevent="statusCollapsed = !statusCollapsed" @click="statusCollapsed = !statusCollapsed">
          <div class="status-row">
            <span class="dot" />
            <div class="status-text" v-show="!statusCollapsed">
              <b>{{ devices.onlineCount }} / {{ devices.devices.length }}</b>
              <span>摄像头在线</span>
            </div>
            <van-icon v-show="statusCollapsed" name="arrow-down" class="status-arrow" />
            <van-icon v-show="!statusCollapsed" name="arrow-up" class="status-arrow" />
          </div>
          <div v-show="!statusCollapsed && devices.devices.length" class="status-bar" aria-hidden="true">
            <i :class="{ warn: devices.devices.length > 0 && devices.onlineCount === 0 }" :style="{ width: onlineRate + '%' }" />
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
  flex-direction: column;
  gap: 9px;
  margin: 4px 14px 6px;
  padding: 10px 12px;
  border-radius: var(--nvr-radius-sm);
  background: var(--nvr-panel-2);
  border: 1px solid var(--nvr-border);
  cursor: pointer;
  transition: all 0.2s ease;
}
.side-status:hover {
  background: var(--nvr-panel);
}
.side-status .status-row {
  display: flex;
  align-items: center;
  gap: 10px;
}
.side-status.collapsed {
  padding: 8px 12px;
}
.side-status.collapsed .status-row {
  justify-content: center;
}
/* 迷你在线率条：一眼看出整体健康度，红色 = 全部离线 */
.side-status .status-bar {
  height: 4px;
  border-radius: var(--nvr-radius-full);
  background: rgba(255, 255, 255, 0.08);
  overflow: hidden;
}
body.light .side-status .status-bar {
  background: rgba(0, 0, 0, 0.08);
}
.side-status .status-bar i {
  display: block;
  height: 100%;
  border-radius: var(--nvr-radius-full);
  background: var(--nvr-grad-accent);
  transition: width 0.3s ease;
}
.side-status .status-bar i.warn {
  background: var(--nvr-danger);
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
.status-arrow {
  font-size: calc(14px * var(--nvr-font-scale, 1));
  color: var(--nvr-text-3);
  margin-left: auto;
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
  background: var(--nvr-accent-soft);
}
.nav-item.on .nav-ico {
  background: var(--nvr-accent-soft-2);
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
