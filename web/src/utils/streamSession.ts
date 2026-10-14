import { http } from '../api/client'
import { server } from '../api/server'

// 每次请求/重建取当前令牌，避免 playlist 内的分片 URL 保留旧令牌。
export function currentStreamUrl(raw: string): string {
  const u = new URL(raw, window.location.href)
  const base = new URL(server.base || window.location.origin, window.location.href)
  if (u.origin !== base.origin || !u.pathname.startsWith('/api/')) return raw
  const token = localStorage.getItem('nvr_token')
  if (token) u.searchParams.set('token', token)
  else u.searchParams.delete('token')
  return u.href
}

let pending: { token: string; base: string; promise: Promise<boolean> } | null = null
// EventSource 无法读取 HTTP 状态：用同源鉴权接口甄别 401 与网络故障。
export function checkStreamSession(): Promise<boolean> {
  const token = localStorage.getItem('nvr_token') || ''
  const base = server.base
  if (!token) return Promise.resolve(false)
  if (pending?.token === token && pending.base === base) return pending.promise
  const promise = http.get('/api/auth/me').then(() => true, (err) => {
    if (base !== server.base || token !== localStorage.getItem('nvr_token')) {
      return !!localStorage.getItem('nvr_token')
    }
    return err.response?.status !== 401
  })
  const entry = { token, base, promise }
  pending = entry
  void promise.finally(() => { if (pending === entry) pending = null })
  return promise
}
