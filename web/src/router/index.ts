import { createRouter, createWebHashHistory } from 'vue-router'
import { backendOk } from '../api'

const routes = [
  { path: '/', redirect: '/login' },
  { path: '/live', name: 'live', component: () => import('../views/LiveView.vue') },
  { path: '/playback', name: 'playback', component: () => import('../views/PlaybackView.vue') },
  { path: '/events', name: 'events', component: () => import('../views/EventsView.vue') },
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
  if (!backendOk) return true
  if (!localStorage.getItem('nvr_token')) return { path: '/login', query: { redirect: to.fullPath } }
  return true
})

export default router
