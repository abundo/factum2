import { reactive, watch } from 'vue'
import { useToast } from '@nuxt/ui/composables'

const STORAGE_KEY_OPEN = 'factum:logpanel-open'
const STORAGE_KEY_HEIGHT = 'factum:logpanel-height'

const MAX_LINES = 1000
const DEFAULT_HEIGHT = 260
const MIN_HEIGHT = 120
const RECONNECT_DELAY = 3000

const state = reactive({
  open: localStorage.getItem(STORAGE_KEY_OPEN) === 'true',
  height: Number(localStorage.getItem(STORAGE_KEY_HEIGHT)) || DEFAULT_HEIGHT,
  lines: [],
  paused: false,
  connected: false,
})

let socket = null
let reconnectTimer = null
let nextId = 0

// connect/disconnect are driven by AppLogPanel's mount lifecycle (itself
// gated by state.open via v-if in AppLayout), so the websocket only exists
// while the panel is actually visible.
function connect() {
  if (socket) return

  const proto = location.protocol === 'https:' ? 'wss:' : 'ws:'
  socket = new WebSocket(`${proto}//${location.host}/api/logs/ws`)

  socket.onopen = () => {
    state.connected = true
  }
  socket.onmessage = (event) => {
    if (state.paused) return
    pushLine(JSON.parse(event.data))
  }
  socket.onclose = () => {
    state.connected = false
    socket = null
    if (state.open) {
      reconnectTimer = setTimeout(connect, RECONNECT_DELAY)
    }
  }
  socket.onerror = () => {
    socket?.close()
  }
}

function pushLine(data) {
  state.lines.push({ id: nextId++, ...data })
  if (state.lines.length > MAX_LINES) {
    state.lines.splice(0, state.lines.length - MAX_LINES)
  }
}

function disconnect() {
  clearTimeout(reconnectTimer)
  reconnectTimer = null
  if (socket) {
    socket.onclose = null
    socket.close()
    socket = null
  }
  state.connected = false
}

export function useLogPanel() {
  function open() {
    state.open = true
    localStorage.setItem(STORAGE_KEY_OPEN, 'true')
  }

  function close() {
    state.open = false
    localStorage.setItem(STORAGE_KEY_OPEN, 'false')
  }

  function toggle() {
    if (state.open) {
      close()
    } else {
      open()
    }
  }

  function setHeight(height) {
    const max = window.innerHeight - 100
    state.height = Math.min(Math.max(height, MIN_HEIGHT), Math.max(max, MIN_HEIGHT))
    localStorage.setItem(STORAGE_KEY_HEIGHT, String(state.height))
  }

  function clear() {
    state.lines = []
  }

  function togglePause() {
    state.paused = !state.paused
  }

  // Client-side lines (e.g. zone-file import problems). These never go
  // through slog; they only appear in this tab's log window.
  function append({ level = 'INFO', message, source = 'web', attrs } = {}) {
    if (!message) return
    pushLine({
      time: new Date().toISOString(),
      level,
      message,
      source,
      attrs,
    })
  }

  return { state, open, close, toggle, setHeight, clear, togglePause, append, connect, disconnect }
}

const TOAST_LEVEL = {
  error: 'ERROR',
  warning: 'WARN',
  warn: 'WARN',
  success: 'INFO',
  info: 'INFO',
  primary: 'INFO',
  secondary: 'INFO',
  neutral: 'INFO',
}

function toastMessage(toast) {
  return [toast.title, toast.description].filter(Boolean).join(': ')
}

let toastBound = false

// Mirror every Nuxt UI toast into this tab's log window. Call once from a
// setup() that shares the app's toast state (App.vue).
export function bindToastToLog() {
  if (toastBound) return
  toastBound = true
  const toast = useToast()
  const { append } = useLogPanel()
  const seen = new Set()
  watch(
    () => toast.toasts.value.map((t) => t.id),
    () => {
      for (const t of toast.toasts.value) {
        if (!t?.id || seen.has(t.id)) continue
        seen.add(t.id)
        const color = String(t.color || '').toLowerCase()
        append({
          level: TOAST_LEVEL[color] || 'INFO',
          source: 'web',
          message: toastMessage(t),
          attrs: t.color ? { toast: t.color } : undefined,
        })
      }
      if (seen.size > MAX_LINES * 2) {
        const live = new Set(toast.toasts.value.map((t) => t.id))
        for (const id of seen) {
          if (!live.has(id)) seen.delete(id)
        }
      }
    },
  )
}
