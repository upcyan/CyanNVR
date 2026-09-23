import { createApp } from 'vue'
import { createPinia } from 'pinia'
import Vant from 'vant'
import 'vant/lib/index.css'
import App from './App.vue'
import router from './router'
import { initApi } from './api'
import './styles/theme.css'

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
    if (!resp.ok) return
    const data = await resp.json()
    const serverVersion = data.version || ''
    if (serverVersion && serverVersion !== FRONTEND_VERSION) {
      let tried = ''
      try {
        tried = sessionStorage.getItem('nvr_nocache_tried') || ''
      } catch { /* ignore */ }
      if (tried === FRONTEND_VERSION) return // 已经试过，放行
      params.set('nocache', '1')
      window.location.replace(
        `${window.location.pathname}?${params.toString()}${window.location.hash}`
      )
    }
  } catch {
    /* ignore */
  }
}

async function bootstrap() {
  await selfCleanCache()
  await initApi()
  const app = createApp(App)
  app.use(createPinia())
  app.use(router)
  app.use(Vant)
  app.mount('#app')
}

bootstrap().catch((err) => {
  console.error('App bootstrap failed:', err)
  document.body.innerHTML =
    '<div style="padding:2rem;color:#fff;text-align:center;background:#171a21;min-height:100vh;display:flex;align-items:center;justify-content:center">' +
    '<div>应用启动失败，请检查网络连接后刷新重试。</div></div>'
})
