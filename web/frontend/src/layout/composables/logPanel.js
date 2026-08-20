import { reactive } from 'vue'

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
    const data = JSON.parse(event.data)
    state.lines.push({ id: nextId++, ...data })
    if (state.lines.length > MAX_LINES) {
      state.lines.splice(0, state.lines.length - MAX_LINES)
    }
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

  return { state, open, close, toggle, setHeight, clear, togglePause, connect, disconnect }
}
