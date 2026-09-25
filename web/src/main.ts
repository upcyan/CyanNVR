import { createApp } from 'vue'
import { createPinia } from 'pinia'
import Vant from 'vant'
import 'vant/lib/index.css'
import App from './App.vue'
import router from './router'
import { initApi } from './api'
import './styles/theme.css'
import { pickerDesktop } from './utils/pickerDesktop'

// ── 启动自清理缓存 ──
// 一次性解决「浏览器残留旧 Service Worker / 预缓存导致界面不更新」：
//  1) 注销所有历史 SW 并清空 CacheStorage（下次请求直达服务器）；
//  2) 若已加载的前端版本 != 服务端版本（说明拿到的是旧缓存），带
//     nocache 参数强制刷新一次；sessionStorage 计数防死循环。
// 只在应用自己的 origin 内生效，清的是本应用的 SW 与缓存。
async function selfCleanCache(): Promise<void> {
  try {
    if ('serviceWorker' in navigator) {
      const regs = await navigator.serviceWorker.getRegistrations()
      for (const r of regs) await r.unregister()
    }
    if (window.caches) {
      const keys = await caches.keys()
      await Promise.all(keys.map((k) => caches.delete(k)))
    }
  } catch {
    /* ignore */
  }

  // 版本自检：构建期由 vite define 把 __APP_VERSION__ 替换成 CoreVersion 字面量
  const FRONTEND_VERSION = __APP_VERSION__ || ''
  if (!FRONTEND_VERSION) return
  const url = new URL('/api/health', window.location.origin)
  const params = new URLSearchParams(window.location.search)
  if (params.get('nocache') === '1') {
    // 刚强制刷新过，仍不一致则放弃，避免循环
    try {
      sessionStorage.setItem('nvr_nocache_tried', FRONTEND_VERSION)
    } catch { /* ignore */ }
    return
  }
  try {
    const ctrl = new AbortController()
    const timer = setTimeout(() => ctrl.abort(), 2500)
    const resp = await fetch(url.toString(), { signal: ctrl.signal, cache: 'no-store' })
    clearTimeout(timer)
    if (!resp.ok) {
      // 服务器可达但自检失败（如 502/503）：老界面继续用，但给出可见提示，
      // 排错时一眼知道「版本自检没跑成」而不是无声放行。
      showVersionCheckNotice('版本自检失败（HTTP ' + resp.status + '），界面可能不是最新，稍后请刷新页面')
      return
    }
    const data = await resp.json()
    const serverVersion = data.version || ''
    if (serverVersion && serverVersion !== FRONTEND_VERSION) {
      let tried = ''
      try {
        tried = sessionStorage.getItem('nvr_nocache_tried') || ''
      } catch { /* ignore */ }
      if (tried === FRONTEND_VERSION) {
        // 已经试过一次强刷仍不一致：不再循环，但让用户知道现状
        showVersionCheckNotice('界面版本（' + FRONTEND_VERSION + '）与服务端（' + serverVersion + '）不一致，请清缓存或联系管理员')
        return
      }
      params.set('nocache', '1')
      window.location.replace(
        `${window.location.pathname}?${params.toString()}${window.location.hash}`
      )
    }
  } catch {
    // 网络失败/超时（2.5s）：老界面继续用，给出可见提示而非静默。
    // 不重试自检——服务器往往正在重启，重试只会拖慢首屏。
    showVersionCheckNotice('无法确认界面版本（服务器暂不可达），如界面异常请稍后刷新')
  }
}

// showVersionCheckNotice：版本自检失败/不一致时的非阻断提示条。
// selfCleanCache 在 Vue 挂载前执行，此时不能用 vant 组件，直接写 DOM。
// sessionStorage 去重：同一次会话内最多提示一次，避免每次刷新都弹。
function showVersionCheckNotice(msg: string) {
  try {
    if (sessionStorage.getItem('nvr_version_notice_shown')) return
    sessionStorage.setItem('nvr_version_notice_shown', '1')
  } catch { /* ignore */ }
  const bar = document.createElement('div')
  bar.textContent = msg
  bar.style.cssText =
    'position:fixed;top:0;left:0;right:0;z-index:99999;padding:10px 16px;'
    + 'background:#5c3a00;color:#ffd591;font-size:13px;text-align:center;'
    + 'box-shadow:0 1px 4px rgba(0,0,0,.4)'
  document.body.appendChild(bar)
  window.setTimeout(() => bar.remove(), 8000)
}

async function bootstrap() {
  await selfCleanCache()
  await initApi()
  const app = createApp(App)
  app.use(createPinia())
  app.use(router)
  app.use(Vant)
  app.directive('picker-desktop', pickerDesktop)
  app.mount('#app')
}

bootstrap().catch((err) => {
  console.error('App bootstrap failed:', err)
  document.body.innerHTML =
    '<div style="padding:2rem;color:#fff;text-align:center;background:#171a21;min-height:100vh;display:flex;align-items:center;justify-content:center">' +
    '<div>应用启动失败，请检查网络连接后刷新重试。</div></div>'
})
