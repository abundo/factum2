<script setup>
import { onMounted, onUnmounted, nextTick, watch } from 'vue'
import { useLogPanel } from './composables/logPanel'

const { state, close, setHeight, clear, togglePause, connect, disconnect } = useLogPanel()

let bodyEl = null
function setBodyRef(el) {
  bodyEl = el
}

// Autoscroll to the newest line as it arrives, unless the user paused the
// stream to read something further up.
watch(
  () => state.lines.length,
  async () => {
    if (state.paused || !bodyEl) return
    await nextTick()
    bodyEl.scrollTop = bodyEl.scrollHeight
  },
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
  return (
    d.toLocaleTimeString(undefined, { hour12: false }) +
    '.' +
    String(d.getMilliseconds()).padStart(3, '0')
  )
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
    <div :ref="setBodyRef" class="flex-1 space-y-0.5 overflow-y-auto px-3 py-2 font-mono text-xs">
      <div v-if="!state.lines.length" class="text-muted">No log events yet</div>
      <div
        v-for="line in state.lines"
        :key="line.id"
        class="flex flex-wrap gap-x-2"
        :class="{
          'text-error': line.level?.toLowerCase() === 'error',
          'text-warning':
            line.level?.toLowerCase() === 'warning' || line.level?.toLowerCase() === 'warn',
        }"
      >
        <span class="text-muted">{{ formatTime(line.time) }}</span>
        <span class="font-semibold uppercase">{{ line.level }}</span>
        <span>{{ line.message }}</span>
        <span v-if="line.attrs && Object.keys(line.attrs).length" class="text-muted">
          <span v-for="(value, key) in line.attrs" :key="key" class="mr-2"
            >{{ key }}={{ value }}</span
          >
        </span>
      </div>
    </div>
  </div>
</template>
