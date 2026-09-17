import { createApp } from 'vue'
import { createPinia } from 'pinia'
import Vant from 'vant'
import 'vant/lib/index.css'
import App from './App.vue'
import router from './router'
import { initApi } from './api'
import './styles/theme.css'

async function bootstrap() {
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
