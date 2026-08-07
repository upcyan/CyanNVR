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
    if (err.response?.status === 401) {
      localStorage.removeItem('nvr_token')
      localStorage.removeItem('nvr_user')
      if (location.hash && !location.hash.includes('/login')) {
        location.hash = '#/login'
      }
    }
    return Promise.reject(err)
  },
)
