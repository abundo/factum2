import { defineStore } from 'pinia'
import { login as apiLogin, logout as apiLogout } from '@/api/auth'
import { getMe } from '@/api/me'
import { useDeviceCredentialsStore } from '@/stores/deviceCredentials'

export const useAuthStore = defineStore('auth', {
  state: () => ({
    user: null,
    loaded: false,
  }),
  getters: {
    isAuthenticated: (state) => state.user !== null,
    isAdmin: (state) => state.user?.roles?.includes('admin') ?? false,
    // canWrite/canRead mirror the backend's RequireWrite/RequireRead role
    // tiers (web/auth.go) - a user with no role at all fails both, and is
    // left with only the dashboard and their own profile.
    canWrite: (state) => {
      const roles = state.user?.roles ?? []
      return roles.includes('admin') || roles.includes('operator')
    },
    canRead: (state) => {
      const roles = state.user?.roles ?? []
      return roles.includes('admin') || roles.includes('operator') || roles.includes('viewer')
    },
    opticalEnabled: (state) => !!state.user?.optical_enabled,
    ipamEnabled: (state) => !!state.user?.ipam_enabled,
    organizationEnabled: (state) => !!state.user?.organization_enabled,
    oxidizedEnabled: (state) => !!state.user?.oxidized_enabled,
  },
  actions: {
    async fetchCurrentUser() {
      try {
        this.user = await getMe()
      } catch {
        this.user = null
      } finally {
        this.loaded = true
      }
    },
    async login(username, password) {
      this.user = await apiLogin(username, password)
      this.loaded = true
    },
    async logout() {
      try {
        await apiLogout()
      } finally {
        this.user = null
        // Device SSH passwords must not outlive the factum session.
        useDeviceCredentialsStore().clearAll()
      }
    },
  },
})
