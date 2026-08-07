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

bootstrap()
