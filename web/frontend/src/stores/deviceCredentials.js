import { defineStore } from 'pinia'

// Session-only: survives refresh within the same tab, cleared when the tab
// closes. Passwords must be retrievable in clear text for SSH driver calls,
// so true secret storage is impossible in the browser - sessionStorage is a
// lifetime tradeoff, not encryption.
const STORAGE_KEY = 'factum.deviceCredentials'

function loadFromSession() {
  try {
    const raw = sessionStorage.getItem(STORAGE_KEY)
    if (!raw) return null
    const parsed = JSON.parse(raw)
    if (!parsed || typeof parsed !== 'object') return null
    const byDevice = {}
    if (parsed.byDevice && typeof parsed.byDevice === 'object') {
      for (const [key, creds] of Object.entries(parsed.byDevice)) {
        if (creds?.username && creds?.password) {
          byDevice[key] = { username: creds.username, password: creds.password }
        }
      }
    }
    const lastUsed =
      parsed.lastUsed?.username && parsed.lastUsed?.password
        ? { username: parsed.lastUsed.username, password: parsed.lastUsed.password }
        : null
    return { byDevice, lastUsed }
  } catch {
    return null
  }
}

function persist(byDevice, lastUsed) {
  try {
    sessionStorage.setItem(STORAGE_KEY, JSON.stringify({ byDevice, lastUsed }))
  } catch {
    // private mode / quota - memory still works for this tab
  }
}

function credsEqual(a, b) {
  return Boolean(a && b && a.username === b.username && a.password === b.password)
}

function deviceKey(deviceId) {
  if (deviceId == null || deviceId === '') return null
  return String(deviceId)
}

function normalizeIds(deviceIds) {
  if (deviceIds == null || deviceIds === '') return []
  const list = Array.isArray(deviceIds) ? deviceIds : [deviceIds]
  return list.map(deviceKey).filter(Boolean)
}

/**
 * Shared device SSH credentials for interactive driver actions (interface
 * refresh/update, VLAN push, ELINE push/delete). Keyed per device so a
 * failed lab login does not wipe credentials for production boxes.
 */
export const useDeviceCredentialsStore = defineStore('deviceCredentials', {
  state: () => {
    const stored = loadFromSession()
    return {
      // deviceId string -> { username, password }
      byDevice: stored?.byDevice ?? {},
      // most recently successful pair; fallback for unknown devices and for
      // multi-device ops that send a single pair (ELINE)
      lastUsed: stored?.lastUsed ?? null,
    }
  },
  actions: {
    getForDevice(deviceId) {
      const key = deviceKey(deviceId)
      if (key) {
        const exact = this.byDevice[key]
        if (exact?.username && exact?.password) {
          return { username: exact.username, password: exact.password }
        }
      }
      // Fallback is lastUsed only - not another device's cached pair. That
      // way a failed lab login (which clears lastUsed) re-prompts instead of
      // silently retrying production credentials from a different box.
      if (this.lastUsed?.username && this.lastUsed?.password) {
        return { username: this.lastUsed.username, password: this.lastUsed.password }
      }
      return null
    },

    /**
     * Username hint for the prompt dialog only (never used as a silent
     * password fallback for a different device).
     */
    usernameHint() {
      if (this.lastUsed?.username) return this.lastUsed.username
      for (const creds of Object.values(this.byDevice)) {
        if (creds?.username) return creds.username
      }
      return ''
    },

    /**
     * Resolve credentials for one or more devices. Prefers an exact match
     * on any listed device, then lastUsed. Does not borrow another
     * device's pair unless it is also lastUsed.
     */
    getForDevices(deviceIds) {
      const keys = normalizeIds(deviceIds)
      for (const key of keys) {
        const exact = this.byDevice[key]
        if (exact?.username && exact?.password) {
          return { username: exact.username, password: exact.password }
        }
      }
      if (this.lastUsed?.username && this.lastUsed?.password) {
        return { username: this.lastUsed.username, password: this.lastUsed.password }
      }
      return null
    },

    remember(deviceIds, username, password) {
      if (!username || !password) return
      const creds = { username, password }
      this.lastUsed = creds
      for (const key of normalizeIds(deviceIds)) {
        this.byDevice[key] = { ...creds }
      }
      persist(this.byDevice, this.lastUsed)
    },

    /**
     * Drop credentials for the given device(s). If lastUsed matches the
     * failed pair, clear it so the next uncached device re-prompts (lab
     * vs prod). Other devices that stored the same pair keep their entries.
     */
    invalidate(deviceIds, username, password) {
      const failed = username && password ? { username, password } : null
      for (const key of normalizeIds(deviceIds)) {
        if (!failed || credsEqual(this.byDevice[key], failed)) {
          delete this.byDevice[key]
        }
      }
      if (failed && credsEqual(this.lastUsed, failed)) {
        this.lastUsed = null
      }
      persist(this.byDevice, this.lastUsed)
    },

    /** Drop all cached credentials (e.g. on logout). */
    clearAll() {
      this.byDevice = {}
      this.lastUsed = null
      try {
        sessionStorage.removeItem(STORAGE_KEY)
      } catch {
        // ignore
      }
    },
  },
})
