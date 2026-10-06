import { createApp } from 'vue'
import { createPinia } from 'pinia'
import Vant from 'vant'
import 'vant/lib/index.css'
import App from './App.vue'
import router from './router'
import { initApi } from './api'
import { bindSessionHooks } from './api/client'
import { useAuthStore } from './stores/auth'
import './styles/theme.css'
import { pickerDesktop } from './utils/pickerDesktop'
import { migrateAppCache, checkBuild } from './utils/buildCache'

async function bootstrap() {
  try { await migrateAppCache() } catch { /* storage may be unavailable in WebView */ }
  await checkBuild()
  await initApi()
  const app = createApp(App)
  app.use(createPinia())
  // bind hooks before router starts its initial navigation
  const auth = useAuthStore()
  bindSessionHooks({
    epoch: () => auth.sessionEpoch,
    onAuthLost: (epoch) => auth.invalidate(epoch),
    onTokenRenewed: (token) => auth.renewToken(token),
  })
  app.use(router)
  app.use(Vant)
  app.directive('picker-desktop', pickerDesktop)
  app.mount('#app')
  document.addEventListener('visibilitychange', () => {
    if (document.visibilityState === 'visible') void checkBuild()
  })
}
bootstrap().catch((err) => {
  console.error('App bootstrap failed:', err)
  document.body.innerHTML = '<div style="padding:2rem;text-align:center">应用启动失败，请检查网络连接后刷新重试。</div>'
})
