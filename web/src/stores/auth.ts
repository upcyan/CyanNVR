import { defineStore } from 'pinia'
import { apiLogin, fetchMe, isBackend, type AuthUser } from '../api'

/** 安全解析持久化的用户信息：损坏的 JSON 不能让整个应用启动失败 */
function loadStoredUser(): AuthUser | null {
  try {
    const raw = localStorage.getItem('nvr_user')
    if (!raw) return null
    const u = JSON.parse(raw)
    if (!u || typeof u !== 'object') return null
    if (typeof u.id !== 'string' || typeof u.role !== 'string') return null
    return u as AuthUser
  } catch {
    return null
  }
}

export const useAuthStore = defineStore('auth', {
  state: () => ({
    user: loadStoredUser(),
    token: localStorage.getItem('nvr_token') || '',
    /**
     * 会话代次：登录/退出/切换服务器时 +1。
     * 在途请求据此判断自己是否属于当前会话——旧会话的 401 或响应
     * 不能影响刚刚建立的新会话（此前会误清新登录的 token）。
     */
    sessionEpoch: 0,
  }),
  getters: {
    /**
     * 已登录。注意：真实后端不可达时**不再**视为已登录——
     * 旧逻辑 `!!s.token || !backendOk` 会在服务器故障时把用户当成
     * 已登录的演示管理员，掩盖故障。
     */
    isLoggedIn: (s) => !!s.token,
    isAdmin: (s) => s.user?.role === 'admin',
    canEdit: (s) => !s.user || (s.user.role !== 'viewer' && s.user.role !== 'user'),
  },
  actions: {
    async login(username: string, password: string) {
      // 真实后端必须真的登录：服务器不可达时返回明确错误，
      // 绝不生成本地演示管理员（那会把故障伪装成登录成功）。
      if (!isBackend()) {
        throw new Error('无法连接服务器，请检查网络连接或服务器地址')
      }
      const { token, user } = await apiLogin(username, password)
      this.token = token
      this.user = user
      this.sessionEpoch++
      localStorage.setItem('nvr_token', token)
      localStorage.setItem('nvr_user', JSON.stringify(user))
      return user
    },
    /** 重新拉取当前用户（关怀模式等用户级设置变更后调用） */
    async refreshMe() {
      if (!isBackend() || !this.token) return
      const epoch = this.sessionEpoch
      try {
        const user = await fetchMe()
        // 期间已切换会话：丢弃这次响应，避免旧身份覆盖新会话
        if (epoch !== this.sessionEpoch) return
        this.user = user
        localStorage.setItem('nvr_user', JSON.stringify(user))
      } catch {
        /* 保留既有用户信息：刷新失败不应登出 */
      }
    },
    /**
     * 令牌续签：服务端在信任窗口内静默换发新 token。
     * 身份未变，因此**不**递增会话代次（否则会误伤在途请求）。
     */
    renewToken(token: string) {
      if (!token) return
      this.token = token
      localStorage.setItem('nvr_token', token)
    },
    /**
     * 会话失效（401 且确实带了凭据）。
     * expectedEpoch 由发起请求的一方传入；与当前会话不一致则忽略，
     * 防止「上一个账号的迟到 401」把新登录的会话清掉。
     */
    invalidate(expectedEpoch?: number) {
      if (expectedEpoch !== undefined && expectedEpoch !== this.sessionEpoch) return false
      this.user = null
      this.token = ''
      this.sessionEpoch++
      localStorage.removeItem('nvr_token')
      localStorage.removeItem('nvr_user')
      return true
    },
    logout() {
      this.invalidate()
    },
  },
})
