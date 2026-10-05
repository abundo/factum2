<script setup>
import { useToast } from '@nuxt/ui/composables'
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { getCapturePlatforms } from '@/api/capture'
import { getDevice } from '@/api/devices'
import HexDump from '@/components/capture/HexDump.vue'
import PacketList from '@/components/capture/PacketList.vue'
import PacketTree from '@/components/capture/PacketTree.vue'
import DeviceInterfacePicker from '@/components/DeviceInterfacePicker.vue'
import { openCaptureWindow } from '@/composables/useCaptureWindow'
import { useCaptureWorker } from '@/composables/useCaptureWorker'
import { useUnsaved } from '@/composables/useFormGuard'
import { useAuthStore } from '@/stores/auth'
import { bytes } from '@/utils/bytes'
import { captureSourceAllowed, platformPrefixes } from '@/utils/captureSource'

// Packet capture: the Arista driver mirrors the chosen interface to the
// switch CPU over eAPI, then factum streams tcpdump's pcap over SSH into
// Wiregasm (Wireshark in WebAssembly), which runs in workers/capture.worker.js.
// CaptureWindowPage shows the same page in a popup (inWindow).
const props = defineProps({ inWindow: Boolean })
const toast = useToast()
const auth = useAuthStore()
const route = useRoute()
const cap = useCaptureWorker()
const { ready, fatal, columns, running, packets, generation } = cap
const received = cap.bytes

const platforms = ref([])
const platformsReady = ref(false)
const pickerOpen = ref(false)
const selected = reactive({
  deviceId: null,
  deviceName: '',
  interfaceId: null,
  interfaceName: '',
})

const form = reactive({
  filter: '',
  max_packets: 100000,
  max_seconds: 600,
  snaplen: 0,
})

const started = ref(null)
const captured = ref({ device: '', interface: '' })
const saved = ref(true)
let fileName = ''

useUnsaved(() => !saved.value && received.value > 24)

const targetLabel = computed(() =>
  selected.deviceName && selected.interfaceName
    ? `${selected.deviceName} · ${selected.interfaceName}`
    : '',
)

function onPick(row) {
  selected.deviceId = row.deviceId
  selected.deviceName = row.deviceName
  selected.interfaceId = row.interfaceId
  selected.interfaceName = row.interfaceName
}

const queryDevice = ref(null)

async function loadQueryTarget() {
  const deviceId = Number(route.query.device) || null
  const interfaceId = Number(route.query.interface) || null
  if (!deviceId || !interfaceId) return
  try {
    queryDevice.value = await getDevice(deviceId)
  } catch {
    // The picker still opens; the names stay blank until a device is chosen.
  }
}

// Apply ?device=&interface= once the driver has said which names it can
// source. A Vlan or a subinterface stays unselected.
function applyQueryTarget() {
  const data = queryDevice.value
  const deviceId = Number(route.query.device) || null
  const interfaceId = Number(route.query.interface) || null
  if (!platformsReady.value || !data || !deviceId || !interfaceId) return
  const iface = (data.interfaces ?? []).find((i) => i.id === interfaceId)
  const prefixes = platformPrefixes(platforms.value, data.platform)
  if (!iface || !captureSourceAllowed(iface.name, prefixes, { parentId: iface.parent_id })) return
  selected.deviceId = deviceId
  selected.deviceName = data.name ?? ''
  selected.interfaceId = interfaceId
  selected.interfaceName = iface.name ?? ''
}

onMounted(() => {
  getCapturePlatforms()
    .then((list) => {
      platforms.value = list ?? []
    })
    .catch(() => {
      toast.add({
        color: 'error',
        title: 'Error',
        description: 'Failed to load capture platforms.',
        duration: 3000,
      })
    })
    .finally(() => {
      platformsReady.value = true
      applyQueryTarget()
    })
  loadQueryTarget().then(() => applyQueryTarget())
})

function start() {
  if (!selected.deviceId || !selected.interfaceId) return
  started.value = new Date()
  captured.value = { device: selected.deviceName, interface: selected.interfaceName }
  const stamp = started.value.toISOString().replace(/[:.]/g, '-')
  fileName = `${selected.deviceName}-${selected.interfaceName}-${stamp}.pcap`
  selectedPacket.value = null
  frame.value = null
  node.value = null
  follow.value = true
  saved.value = false
  cap.start({
    device_id: selected.deviceId,
    interface_id: selected.interfaceId,
    filter: form.filter.trim(),
    max_packets: Number(form.max_packets) || 0,
    max_seconds: Number(form.max_seconds) || 0,
    snaplen: Number(form.snaplen) || 0,
  })
}

cap.onEnded((error) => {
  if (error) toast.add({ title: error, color: 'error' })
})

async function download() {
  const url = URL.createObjectURL(await cap.pcap())
  const a = document.createElement('a')
  a.href = url
  a.download = fileName
  a.click()
  URL.revokeObjectURL(url)
  saved.value = true
}

// The display filter (Wireshark syntax) is checked as you type and
// applied once it is valid.
const display = ref('')
const applied = ref('')
const filterError = ref('')
let checkTimer = null
watch(display, (f) => {
  clearTimeout(checkTimer)
  checkTimer = setTimeout(async () => {
    const t = f.trim()
    const r = t ? await cap.checkFilter(t) : { ok: true }
    filterError.value = r.ok ? '' : r.error || 'invalid filter'
    if (r.ok) applied.value = t
  }, 300)
})

const follow = ref(true)
const listVersion = computed(() => `${generation.value}/${applied.value}`)
const loadRows = (skip, limit) => cap.frames(applied.value, skip, limit)

const selectedPacket = ref(null)
const frame = ref(null)
const node = ref(null)
const treeOpen = reactive(new Set())

async function select(n) {
  selectedPacket.value = n
  follow.value = false
  try {
    frame.value = await cap.frame(n)
    node.value = null
  } catch (err) {
    toast.add({ title: err.message, color: 'error' })
  }
}

const hexSource = computed(() => frame.value?.sources[node.value?.source ?? 0])

const SPLIT_KEY = 'capture-split'
const split = reactive({ top: 50, left: 50 })
try {
  Object.assign(split, JSON.parse(localStorage.getItem(SPLIT_KEY)) ?? {})
} catch {
  // defaults
}
watch(split, () => {
  try {
    localStorage.setItem(SPLIT_KEY, JSON.stringify(split))
  } catch {
    // not remembered
  }
})
const panes = ref(null)
const bottom = ref(null)
function drag(key, event) {
  const el = key === 'top' ? panes.value : bottom.value
  const bar = event.currentTarget
  bar.setPointerCapture(event.pointerId)
  const move = (e) => {
    const r = el.getBoundingClientRect()
    const pct =
      key === 'top'
        ? ((e.clientY - r.top) / r.height) * 100
        : ((e.clientX - r.left) / r.width) * 100
    split[key] = Math.min(90, Math.max(10, pct))
  }
  const up = () => {
    bar.removeEventListener('pointermove', move)
    bar.removeEventListener('pointerup', up)
  }
  bar.addEventListener('pointermove', move)
  bar.addEventListener('pointerup', up)
}
</script>

<template>
  <div
    class="flex min-h-[36rem] flex-col gap-3"
    :class="props.inWindow ? 'h-full' : 'h-[calc(100vh-7rem)]'"
  >
    <form class="flex flex-wrap items-end gap-3" @submit.prevent="start">
      <UFormField label="Device / interface">
        <UButton
          type="button"
          variant="outline"
          icon="i-lucide-cable"
          class="max-w-80"
          :disabled="running || !platformsReady"
          @click="pickerOpen = true"
        >
          <span class="truncate">{{ targetLabel || 'Select device / interface' }}</span>
        </UButton>
      </UFormField>
      <UFormField label="Capture filter" class="min-w-64 flex-1">
        <UInput
          v-model="form.filter"
          class="w-full font-mono"
          placeholder="host 192.0.2.1 and port 53"
          :disabled="running"
        />
      </UFormField>
      <UFormField label="Packets">
        <UInput v-model="form.max_packets" type="number" min="1" class="w-28" :disabled="running" />
      </UFormField>
      <UFormField label="Seconds">
        <UInput
          v-model="form.max_seconds"
          type="number"
          min="1"
          max="3600"
          class="w-24"
          :disabled="running"
        />
      </UFormField>
      <UFormField label="Bytes/packet" title="0 captures whole packets">
        <UInput
          v-model="form.snaplen"
          type="number"
          min="0"
          max="65535"
          class="w-24"
          :disabled="running"
        />
      </UFormField>
      <div class="flex items-center gap-2">
        <UButton
          v-if="!running"
          type="submit"
          icon="i-lucide-play"
          :disabled="!selected.deviceId || !selected.interfaceId || !ready || !auth.canWrite"
          >Start</UButton
        >
        <UButton v-else color="error" icon="i-lucide-square" @click="cap.stop()">Stop</UButton>
        <UButton
          v-if="!running && received > 24"
          variant="outline"
          icon="i-lucide-download"
          @click="download"
          >pcap</UButton
        >
        <UButton
          v-if="!props.inWindow"
          icon="i-lucide-external-link"
          color="neutral"
          variant="ghost"
          title="Capture in a window of its own, while you use the rest of the GUI"
          :disabled="!selected.deviceId || !selected.interfaceId"
          @click="openCaptureWindow(selected.deviceId, selected.interfaceId)"
          >Open in window</UButton
        >
      </div>
    </form>

    <p v-if="!auth.canWrite" class="text-sm text-muted">
      Starting a capture mirrors the interface on the device. That takes an operator.
    </p>
    <p v-else class="text-sm text-muted">
      The interface is mirrored to the switch CPU for this capture. The mirror is removed when the
      capture stops.
    </p>

    <UAlert v-if="fatal" color="error" variant="subtle" :title="fatal" />
    <div v-else-if="!ready" class="text-sm text-muted">
      <UIcon name="i-lucide-loader-circle" class="animate-spin align-middle" /> Loading Wireshark
      (about 20 MB)…
    </div>

    <div class="flex flex-wrap items-center gap-3">
      <UInput
        v-model="display"
        icon="i-lucide-filter"
        placeholder="Display filter, e.g. dns || tcp.port == 443"
        aria-label="Display filter"
        size="sm"
        class="w-96 max-w-full font-mono"
        :color="filterError ? 'error' : undefined"
        :highlight="!!filterError"
        :ui="{ trailing: 'pe-1' }"
        @keydown.enter.prevent
        @keydown.esc="display = ''"
      >
        <template v-if="display" #trailing>
          <UButton
            color="neutral"
            variant="link"
            size="sm"
            icon="i-lucide-x"
            aria-label="Clear filter"
            title="Clear filter"
            @click="display = ''"
          />
        </template>
      </UInput>
      <USwitch v-model="follow" label="Follow" size="sm" />
      <span v-if="started && running && !received" class="text-sm text-muted">
        <UIcon name="i-lucide-loader-circle" class="me-1 animate-spin align-middle" />
        Setting up the monitor session on {{ captured.interface }}…
      </span>
      <span v-else-if="started" class="text-sm text-muted">
        <span v-if="running" class="me-1 inline-block size-2 animate-pulse rounded-full bg-error" />
        {{ packets }} packets, {{ bytes(received) }} · {{ captured.device }} ·
        {{ captured.interface }}
      </span>
      <span v-if="filterError" class="text-sm text-error">{{ filterError }}</span>
      <span class="ms-auto text-sm text-muted">
        <UIcon name="i-lucide-circle-help" class="align-middle" />
        Help:
        <a
          href="https://wiki.wireshark.org/CaptureFilters"
          target="_blank"
          rel="noopener noreferrer"
          class="text-primary hover:underline"
          title="Capture filters (pcap-filter syntax): applied on the device, decide which packets are captured at all"
          >capture filters</a
        >
        ·
        <a
          href="https://wiki.wireshark.org/DisplayFilters"
          target="_blank"
          rel="noopener noreferrer"
          class="text-primary hover:underline"
          title="Display filters (Wireshark syntax): applied in the browser, only hide packets already captured"
          >display filters</a
        >
      </span>
    </div>

    <div ref="panes" class="flex min-h-0 flex-1 flex-col">
      <div class="flex min-h-0 flex-col" :style="{ height: `${split.top}%` }">
        <PacketList
          v-if="ready"
          v-model:follow="follow"
          :columns="columns"
          :load="loadRows"
          :version="listVersion"
          :selected="selectedPacket"
          @select="select"
        />
        <div v-else class="flex-1 rounded border border-default" />
      </div>
      <div
        class="splitter h-3 cursor-row-resize"
        title="Drag to resize, double-click to reset"
        @pointerdown.prevent="drag('top', $event)"
        @dblclick="split.top = 50"
      />
      <div ref="bottom" class="flex min-h-0 flex-1 flex-col gap-3 lg:flex-row lg:gap-0">
        <div
          class="min-h-0 flex-1 overflow-auto rounded border border-default p-2 lg:w-(--left) lg:flex-none"
          :style="{ '--left': `${split.left}%` }"
        >
          <PacketTree
            v-if="frame"
            :key="frame.number"
            :nodes="frame.tree"
            :selected="node"
            :open="treeOpen"
            @select="node = $event"
          />
          <div v-else class="p-2 text-sm text-muted">Select a packet.</div>
        </div>
        <div
          class="splitter hidden w-3 cursor-col-resize lg:block"
          title="Drag to resize, double-click to reset"
          @pointerdown.prevent="drag('left', $event)"
          @dblclick="split.left = 50"
        />
        <div class="min-h-0 min-w-0 flex-1 overflow-auto rounded border border-default p-2">
          <HexDump
            v-if="hexSource"
            :data="hexSource.data"
            :start="node?.start ?? 0"
            :length="node?.length ?? 0"
          />
        </div>
      </div>
    </div>

    <DeviceInterfacePicker
      v-model:open="pickerOpen"
      mode="service"
      hide-subinterfaces
      :platforms="platforms"
      :device-id="selected.deviceId"
      :interface-id="selected.interfaceId"
      @select="onPick"
    />
  </div>
</template>

<style scoped>
.splitter {
  position: relative;
  touch-action: none;
}
.splitter::after {
  content: '';
  position: absolute;
  inset: 0;
  margin: auto;
  border-radius: 9999px;
  background: var(--ui-border-accented);
}
.splitter.cursor-row-resize::after {
  width: 3rem;
  height: 3px;
}
.splitter.cursor-col-resize::after {
  width: 3px;
  height: 3rem;
}
.splitter:hover::after {
  background: var(--ui-primary);
}
</style>
