import { defineStore } from 'pinia'
import { apiLogin, backendOk, type AuthUser } from '../api'

export const useAuthStore = defineStore('auth', {
  state: () => ({
    user: JSON.parse(localStorage.getItem('nvr_user') || 'null') as AuthUser | null,
    token: localStorage.getItem('nvr_token') || '',
  }),
  getters: {
    isLoggedIn: (s) => !!s.token || !backendOk,
    isAdmin: (s) => s.user?.role === 'admin',
    canEdit: (s) => !s.user || (s.user.role !== 'viewer' && s.user.role !== 'user'),
  },
  actions: {
    async login(username: string, password: string) {
      if (!backendOk) {
        const u: AuthUser = { id: 'local', username: username || 'admin', role: 'admin' }
        this.user = u
        this.token = 'demo'
        localStorage.setItem('nvr_user', JSON.stringify(u))
        localStorage.setItem('nvr_token', 'demo')
        return u
      }
      const { token, user } = await apiLogin(username, password)
      this.token = token
      this.user = user
      localStorage.setItem('nvr_token', token)
      localStorage.setItem('nvr_user', JSON.stringify(user))
      return user
    },
    logout() {
      this.user = null
      this.token = ''
      localStorage.removeItem('nvr_token')
      localStorage.removeItem('nvr_user')
    },
  },
})
