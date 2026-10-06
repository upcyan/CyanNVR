import { createRouter, createWebHashHistory } from 'vue-router'
import { isBackend } from '../api'

const routes = [
  { path: '/', redirect: '/login' },
  { path: '/live', name: 'live', component: () => import('../views/LiveView.vue') },
  { path: '/playback', name: 'playback', component: () => import('../views/PlaybackView.vue') },
  // 事件中心已并入录像管理页（?tab=events）；旧直达链接重定向兼容
  { path: '/events', redirect: { path: '/playback', query: { tab: 'events' } } },
  { path: '/settings', name: 'settings', component: () => import('../views/SettingsView.vue') },
  { path: '/login', name: 'login', component: () => import('../views/LoginView.vue'), meta: { public: true } },
  { path: '/server', name: 'server', component: () => import('../views/ServerView.vue'), meta: { public: true } },
]

const router = createRouter({
  history: createWebHashHistory(),
  routes,
})

router.beforeEach((to) => {
  if (to.meta.public) return true
  // 后端不可达时不再放行到受保护页面：那会让用户看到空数据界面而无从判断
  // 是「没有设备」还是「服务器连不上」。统一回到登录页（登录页会给出连接提示）。
  if (!isBackend()) {
    if (to.path === '/login') return true
    return { path: '/login', query: { redirect: to.fullPath, offline: '1' } }
  }
  if (!localStorage.getItem('nvr_token')) return { path: '/login', query: { redirect: to.fullPath } }
  return true
})

export default router
