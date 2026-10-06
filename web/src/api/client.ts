import axios from 'axios'
import { server } from './server'

export const http = axios.create({ timeout: 15000 })

/**
 * 会话代次与「当前会话」的桥接。
 *
 * auth store 是会话的真相源，但拦截器在 store 之外运行，因此这里用一个
 * 轻量注册表同步必要信息：
 *   · epochOf()  —— 发起请求时快照会话代次；
 *   · onAuthLost() —— 401 且确实带凭据时，通知 store 失效会话（带代次校验）；
 *   · onTokenRenewed() —— 信任窗口内服务端续签，同步给 store 与存储。
 * 不用直接 import store，避免 api 层与 pinia 循环依赖。
 */
let epochProvider: () => number = () => 0
let authLostHandler: (expectedEpoch: number) => void = () => {}
let tokenRenewedHandler: (token: string) => void = () => {}

export function bindSessionHooks(hooks: {
  epoch: () => number
  onAuthLost: (expectedEpoch: number) => void
  onTokenRenewed: (token: string) => void
}) {
  epochProvider = hooks.epoch
  authLostHandler = hooks.onAuthLost
  tokenRenewedHandler = hooks.onTokenRenewed
}

http.interceptors.request.use((cfg) => {
  cfg.baseURL = server.base
  cfg.headers['Cache-Control'] = 'no-cache'
  const token = localStorage.getItem('nvr_token')
  if (token) cfg.headers.Authorization = `Bearer ${token}`
  // 快照发出请求时的会话代次，供响应侧判断是否属于当前会话
  ;(cfg as any).__epoch = epochProvider()
  return cfg
})

http.interceptors.response.use(
  (r) => {
    // 免登录信任窗口：token 过期但仍在窗口内时，服务端自动续签，
    // 新 token 经此响应头下发。这里同步到 store 与本地存储，用户全程无感。
    const renewed = r.headers?.['x-renewed-token']
    if (typeof renewed === 'string' && renewed) {
      tokenRenewedHandler(renewed)
    }
    return r
  },
  (err) => {
    // 只有「确实带了 token 却被服务端拒绝」才判为会话失效。
    // 登录/首屏竞态时本地可能还没 token，此时不能清空并跳登录，
    // 否则刚拿到的 token 会被连带删除，界面卡在「暂无设备」。
    const hadToken = !!localStorage.getItem('nvr_token')
    const sentBearer = !!err?.config?.headers?.Authorization
    if (err.response?.status === 401 && hadToken && sentBearer) {
      // 带上发起请求时的代次：若期间已重新登录/切换账号，
      // 这个迟到的 401 属于旧会话，不能清掉新会话。
      const epoch = (err.config as any)?.__epoch
      const expected = typeof epoch === 'number' ? epoch : epochProvider()
      if (expected !== epochProvider()) return Promise.reject(err)
      authLostHandler(expected)
      if (location.hash && !location.hash.includes('/login')) {
        location.hash = '#/login'
      }
    }
    return Promise.reject(err)
  },
)
