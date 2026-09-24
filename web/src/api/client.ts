import axios from 'axios'
import { server } from './server'

export const http = axios.create({ timeout: 15000 })

http.interceptors.request.use((cfg) => {
  cfg.baseURL = server.base
  const token = localStorage.getItem('nvr_token')
  if (token) cfg.headers.Authorization = `Bearer ${token}`
  return cfg
})

http.interceptors.response.use(
  (r) => r,
  (err) => {
    // 只有「确实带了 token 却被服务端拒绝」才判为会话失效。
    // 登录/首屏竞态时本地可能还没 token，此时不能清空并跳登录，
    // 否则刚拿到的 token 会被连带删除，界面卡在「暂无设备」。
    const hadToken = !!localStorage.getItem('nvr_token')
    const sentBearer = !!err?.config?.headers?.Authorization
    if (err.response?.status === 401 && hadToken && sentBearer) {
      localStorage.removeItem('nvr_token')
      localStorage.removeItem('nvr_user')
      if (location.hash && !location.hash.includes('/login')) {
        location.hash = '#/login'
      }
    }
    return Promise.reject(err)
  },
)
