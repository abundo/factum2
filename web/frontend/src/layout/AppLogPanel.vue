<script setup>
import { onMounted, onUnmounted, watch } from 'vue'
import { formatDateTime } from '@/utils/datetime'
import { useLogPanel } from './composables/logPanel'

const { state, close, setHeight, clear, togglePause, connect, disconnect } = useLogPanel()

// Labels match SyncOverviewPage's targetInfo so a librenms/netbox sync line
// reads the same here as on the job overview tiles. Unknown sources (or a
// worker command name we haven't listed) render as-is.
const SOURCE_LABELS = {
  becs: 'BECS',
  lime: 'Lime',
  netbox: 'Netbox',
  dns: 'DNS',
  icinga: 'Icinga',
  librenms: 'LibreNMS',
  oxidized: 'Oxidized',
  prometheus: 'Prometheus',
  housekeeping: 'Housekeeping',
  'device-sync': 'Device sync',
  hub: 'Hub',
  job: 'Job',
  web: 'Web',
  eline: 'ELINE',
}

// Already represented by the source badge (command/source/target) or too
// noisy for the tail (id/stream are hub-transport internals).
const HIDDEN_ATTRS = new Set(['id', 'command', 'stream', 'source', 'target'])

let bodyEl = null
function setBodyRef(el) {
  bodyEl = el
}

// Scroll once after the DOM catches up. Watching the newest id (not the
// length) still follows the tail once the buffer is full and a batch only
// scrolls once, so a reload jumps to the end instead of stepping there.
watch(
  () => state.lines.at(-1)?.id,
  () => {
    if (state.paused || !bodyEl) return
    bodyEl.scrollTop = bodyEl.scrollHeight
  },
  { flush: 'post' },
)

function startResize(event) {
  event.preventDefault()
  const startY = event.clientY
  const startHeight = state.height

  function onMove(moveEvent) {
    setHeight(startHeight + (startY - moveEvent.clientY))
  }
  function onUp() {
    window.removeEventListener('mousemove', onMove)
    window.removeEventListener('mouseup', onUp)
  }
  window.addEventListener('mousemove', onMove)
  window.addEventListener('mouseup', onUp)
}

function formatTime(iso) {
  const d = new Date(iso)
  return formatDateTime(iso) + '.' + String(d.getMilliseconds()).padStart(3, '0')
}

function lineSource(line) {
  return line.source || line.attrs?.command || line.attrs?.source || line.attrs?.target || 'web'
}

function sourceLabel(line) {
  const source = lineSource(line)
  return SOURCE_LABELS[source] || source
}

function commandFinished(line) {
  return line.message === 'command finished'
}

function commandExitCode(line) {
  const raw = line.attrs?.exit_code
  if (raw == null || raw === '') return null
  const code = Number(raw)
  return Number.isFinite(code) ? code : null
}

function commandErr(line) {
  return String(line.attrs?.err || '').trim()
}

function commandFailed(line) {
  if (!commandFinished(line)) return false
  const exitCode = commandExitCode(line)
  return Boolean(commandErr(line) || (exitCode != null && exitCode !== 0))
}

// Hub exit records arrive as message "command finished" plus slog attrs
// err=/exit_code=. Fold those into a single status so the tail reads like
// the rest of the job stream instead of dumping empty key=value pairs.
function lineMessage(line) {
  if (!commandFinished(line)) return line.message
  if (!commandFailed(line)) return 'command finished successfully'
  const err = commandErr(line)
  const exitCode = commandExitCode(line)
  if (err && exitCode != null && exitCode !== 0) {
    return `command failed: ${err} (exit code ${exitCode})`
  }
  if (err) return `command failed: ${err}`
  if (exitCode != null) return `command failed (exit code ${exitCode})`
  return 'command failed'
}

function visibleAttrs(line) {
  const attrs = line.attrs
  if (!attrs) return null
  const hidden = new Set(HIDDEN_ATTRS)
  if (commandFinished(line)) {
    hidden.add('err')
    hidden.add('exit_code')
  }
  const entries = Object.entries(attrs).filter(
    ([key, value]) => !hidden.has(key) && value !== '' && value != null,
  )
  return entries.length ? Object.fromEntries(entries) : null
}

onMounted(connect)
onUnmounted(disconnect)
</script>

<template>
  <div
    class="flex w-full shrink-0 flex-col overflow-hidden border-t border-default bg-default"
    :style="{ height: state.height + 'px' }"
  >
    <div class="h-1 cursor-row-resize hover:bg-primary/30" @mousedown="startResize"></div>
    <div class="flex items-center gap-2 border-b border-default px-3 py-2">
      <UIcon name="i-lucide-terminal" class="size-4" />
      <span class="text-sm font-medium">Logs</span>
      <span
        class="rounded px-1.5 py-0.5 text-xs"
        :class="state.connected ? 'bg-success/10 text-success' : 'bg-elevated text-muted'"
      >
        {{ state.connected ? 'connected' : 'reconnecting…' }}
      </span>
      <div class="ml-auto flex items-center gap-1">
        <UButton
          :icon="state.paused ? 'i-lucide-play' : 'i-lucide-pause'"
          variant="ghost"
          color="neutral"
          size="xs"
          :title="state.paused ? 'Resume' : 'Pause'"
          @click="togglePause"
        />
        <UButton
          icon="i-lucide-trash-2"
          variant="ghost"
          color="neutral"
          size="xs"
          title="Clear"
          @click="clear"
        />
        <UButton
          icon="i-lucide-x"
          variant="ghost"
          color="neutral"
          size="xs"
          title="Close"
          @click="close"
        />
      </div>
    </div>
    <div
      :ref="setBodyRef"
      class="flex-1 space-y-0.5 overflow-y-auto px-3 py-2 font-mono text-xs [overflow-anchor:none]"
    >
      <div v-if="!state.lines.length" class="text-muted">No log events yet</div>
      <div
        v-for="line in state.lines"
        :key="line.id"
        class="flex flex-wrap gap-x-2"
        :class="{
          'text-error': line.level?.toLowerCase() === 'error' || commandFailed(line),
          'text-warning':
            line.level?.toLowerCase() === 'warning' || line.level?.toLowerCase() === 'warn',
        }"
      >
        <span class="text-muted">{{ formatTime(line.time) }}</span>
        <span
          class="shrink-0 rounded bg-elevated px-1.5 py-0.5 font-medium text-primary"
          :data-log-source="lineSource(line)"
          :title="lineSource(line)"
          >{{ sourceLabel(line) }}</span
        >
        <span class="font-semibold uppercase">{{ line.level }}</span>
        <span>{{ lineMessage(line) }}</span>
        <span v-if="visibleAttrs(line)" class="text-muted">
          <span v-for="(value, key) in visibleAttrs(line)" :key="key" class="mr-2">
            {{ key }}={{ value }}
          </span>
        </span>
      </div>
    </div>
  </div>
</template>
