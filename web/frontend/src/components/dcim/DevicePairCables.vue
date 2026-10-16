<script setup>
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useToast } from '@nuxt/ui/composables'
import { useConfirm } from '@/composables/useConfirm'
import { useFormGuard } from '@/composables/useFormGuard'
import { wideModal } from '@/utils/form'
import {
  createConnection,
  deleteConnection,
  getConnectionPair,
  updateConnection,
} from '@/api/connections'
import { compareInterfaceName } from '@/utils/interfaceNames'

const props = defineProps({
  deviceIds: { type: Array, default: () => [0] },
  deviceItems: { type: Array, default: () => [] },
  canWrite: { type: Boolean, default: false },
})

const emit = defineEmits(['update:deviceIds'])

const toast = useToast()
const { confirmDelete } = useConfirm()

const stage = ref(null)
const scroller = ref(null)
const loading = ref(false)
const error = ref(null)
const byId = ref({})
const selected = ref(null)
const saving = ref(false)
const editLabel = ref('')
const editA = ref(null)
const editB = ref(null)
const points = ref({})
const deviceDrag = ref(null)
const deviceHover = ref(-1)
const ringTip = ref(null)
const createOpen = ref(false)
const createFrom = ref(0)
const createTo = ref(0)
const pickA = ref(null)
const pickB = ref(null)
let dragCleanup = null
let scrollRaf = 0
let lastPointer = { x: 0, y: 0 }
let loadGen = 0

const columns = computed(() => (props.deviceIds.length ? props.deviceIds : [0]))

function errMsg(err, fallback) {
  return err.response?.data?.error ?? fallback
}

function deviceIdFromPicker(v) {
  if (v == null) return 0
  if (typeof v === 'object') return v.id || 0
  return v || 0
}

function emitIds(ids) {
  emit('update:deviceIds', ids.length ? ids : [0])
}

function deviceChoices(index) {
  const taken = new Set(columns.value.filter((id, i) => id && i !== index))
  return props.deviceItems.filter((item) => !taken.has(item.id))
}

function setDevice(index, id) {
  const nextId = id || 0
  if (nextId && columns.value.some((existing, i) => i !== index && existing === nextId)) return
  const next = columns.value.slice()
  next[index] = nextId
  emitIds(next)
}

function addColumn() {
  emitIds([...columns.value, 0])
}

function removeColumn(index) {
  emitIds(columns.value.filter((_, i) => i !== index))
}

function follow(index, iface) {
  const peer = iface.connection?.peer_device_id
  if (!peer) return
  if (columns.value[index + 1] === peer) return
  const existing = columns.value.indexOf(peer)
  if (existing !== -1 && existing <= index) return
  emitIds([...columns.value.slice(0, index + 1), peer])
}

function isPhysical(iface) {
  const t = (iface.type || '').toLowerCase()
  return t !== 'virtual' && t !== 'lag'
}

function sortedIfaces(list) {
  return [...(list ?? [])].sort((a, b) => compareInterfaceName(a.name, b.name) || a.id - b.id)
}

function deviceAt(index) {
  const id = columns.value[index]
  return id ? byId.value[id] || null : null
}

function portsAt(index) {
  return sortedIfaces(deviceAt(index)?.interfaces).filter(
    (iface) => isPhysical(iface) && iface.connection,
  )
}

function dialogPorts(deviceId) {
  const dev = byId.value[deviceId]
  return sortedIfaces(dev?.interfaces).filter(isPhysical)
}

function deviceName(id) {
  return byId.value[id]?.name || 'Device'
}

function tipText(iface) {
  if (!iface?.connection) return 'Not connected'
  return `${iface.connection.peer_device_name} ${iface.connection.peer_interface_name}`
}

function showRingTip(e, iface) {
  ringTip.value = { x: e.clientX + 14, y: e.clientY + 16, text: tipText(iface) }
}

function cableSide(iface, index) {
  const peer = iface.connection?.peer_device_id
  if (!peer) return null
  if (index > 0 && columns.value[index - 1] === peer) return 'left'
  if (columns.value[index + 1] === peer) return 'right'
  return null
}

function portKey(index, ifaceId, dir) {
  return `${index}:${ifaceId}:${dir}`
}

function handleClass(iface) {
  const parts = ['relative z-10 size-5 shrink-0 rounded-full border-2 shadow-sm touch-none']
  parts.push(
    iface.connection?.source === 'netbox'
      ? 'cursor-pointer border-warning bg-warning'
      : 'cursor-pointer border-primary bg-primary',
  )
  return parts.join(' ')
}

function showHandle(iface, index, dir) {
  const side = cableSide(iface, index)
  if (side) return side === dir
  return !!iface.connection && dir === 'right'
}

function columnClass(index) {
  const d = deviceDrag.value
  if (!d?.moved) return ''
  if (d.fromIndex === index) return 'ring-2 ring-primary'
  if (deviceHover.value === index && columns.value[index]) return 'ring-2 ring-success'
  return ''
}

function load() {
  const ids = [...new Set(columns.value.filter((id) => id > 0))]
  if (!ids.length) {
    byId.value = {}
    selected.value = null
    error.value = null
    loading.value = false
    return Promise.resolve()
  }
  const gen = ++loadGen
  loading.value = true
  error.value = null
  return Promise.all(
    ids.map((id) =>
      getConnectionPair(id, id)
        .then((data) => ({ id, device: data.device_a }))
        .catch((err) => ({ id, err })),
    ),
  )
    .then((rows) => {
      if (gen !== loadGen) return
      const next = {}
      const failed = rows.find((row) => row.err)
      for (const row of rows) {
        if (row.device) next[row.id] = row.device
      }
      byId.value = next
      error.value = failed ? errMsg(failed.err, 'Failed to load interfaces.') : null
      if (selected.value) {
        const still = findCable(selected.value.id)
        selected.value = still
        if (still) syncEditFrom(still)
      }
    })
    .finally(() => {
      if (gen !== loadGen) return
      loading.value = false
      nextTick(measure)
    })
}

function adjacentCables() {
  const out = []
  for (let i = 0; i < columns.value.length - 1; i++) {
    const left = deviceAt(i)
    const rightId = columns.value[i + 1]
    if (!left || !rightId) continue
    for (const iface of left.interfaces || []) {
      const link = iface.connection
      if (!link || link.peer_device_id !== rightId) continue
      out.push({
        id: link.id,
        label: link.label || '',
        source: link.source,
        leftIndex: i,
        device_a_id: left.id,
        device_a_name: left.name,
        interface_a_id: iface.id,
        device_b_id: rightId,
        device_b_name: link.peer_device_name,
        interface_b_id: link.peer_interface_id,
      })
    }
  }
  return out
}

const drawnCables = computed(() => adjacentCables())

function findCable(id) {
  return drawnCables.value.find((c) => c.id === id) || null
}

function syncEditFrom(cable) {
  editLabel.value = cable.label || ''
  editA.value = cable.interface_a_id
  editB.value = cable.interface_b_id
}

function measure() {
  const root = stage.value
  if (!root) {
    points.value = {}
    return
  }
  const box = root.getBoundingClientRect()
  const next = {}
  for (const el of root.querySelectorAll('[data-port-handle]')) {
    const key = el.getAttribute('data-port-key')
    const r = el.getBoundingClientRect()
    next[key] = { x: r.left + r.width / 2 - box.left, y: r.top + r.height / 2 - box.top }
  }
  points.value = next
}

function cablePath(c) {
  const p1 = points.value[portKey(c.leftIndex, c.interface_a_id, 'right')]
  const p2 = points.value[portKey(c.leftIndex + 1, c.interface_b_id, 'left')]
  if (!p1 || !p2) return ''
  const dx = Math.max(40, Math.abs(p2.x - p1.x) / 2)
  const dir = p2.x >= p1.x ? 1 : -1
  return `M ${p1.x} ${p1.y} C ${p1.x + dx * dir} ${p1.y}, ${p2.x - dx * dir} ${p2.y}, ${p2.x} ${p2.y}`
}

const deviceDragPath = computed(() => {
  const d = deviceDrag.value
  if (!d?.moved || !stage.value) return ''
  const col = stage.value.querySelector(`[data-device-col="${d.fromIndex}"]`)
  if (!col) return ''
  const box = stage.value.getBoundingClientRect()
  const r = col.getBoundingClientRect()
  const x1 = r.left + r.width / 2 - box.left
  const y1 = r.top + 28 - box.top
  const x2 = d.x - box.left
  const y2 = d.y - box.top
  const dx = Math.max(40, Math.abs(x2 - x1) / 2)
  return `M ${x1} ${y1} C ${x1 + dx} ${y1}, ${x2 - dx} ${y2}, ${x2} ${y2}`
})

function stopAutoScroll() {
  if (!scrollRaf) return
  cancelAnimationFrame(scrollRaf)
  scrollRaf = 0
}

function overflowAncestors(el) {
  const out = []
  let n = el
  while (n && n !== document.documentElement) {
    const style = getComputedStyle(n)
    const oy = style.overflowY
    if (
      (oy === 'auto' || oy === 'scroll' || oy === 'overlay') &&
      n.scrollHeight > n.clientHeight + 1
    ) {
      out.push(n)
    }
    n = n.parentElement
  }
  return out
}

function autoScrollStep() {
  scrollRaf = 0
  if (!deviceDrag.value) return
  const x = lastPointer.x
  const y = lastPointer.y
  const edge = 64
  let moved = false
  const nodes = overflowAncestors(scroller.value || stage.value)
  if (scroller.value && !nodes.includes(scroller.value)) nodes.unshift(scroller.value)
  for (const el of nodes) {
    const box = el.getBoundingClientRect()
    let dx = 0
    let dy = 0
    if (y < box.top + edge) dy = -Math.max(4, Math.min(28, (box.top + edge - y) / 3))
    else if (y > box.bottom - edge) dy = Math.max(4, Math.min(28, (y - (box.bottom - edge)) / 3))
    if (x < box.left + edge) dx = -Math.max(4, Math.min(28, (box.left + edge - x) / 3))
    else if (x > box.right - edge) dx = Math.max(4, Math.min(28, (x - (box.right - edge)) / 3))
    if (!dx && !dy) continue
    el.scrollTop += dy
    el.scrollLeft += dx
    moved = true
  }
  if (!moved) return
  measure()
  if (deviceDrag.value) deviceDrag.value = { ...deviceDrag.value, x: lastPointer.x, y: lastPointer.y }
  scrollRaf = requestAnimationFrame(autoScrollStep)
}

function columnUnderPoint(x, y) {
  const stack = document.elementsFromPoint?.(x, y) ?? [document.elementFromPoint(x, y)]
  for (const el of stack) {
    const col = el?.closest?.('[data-device-col]')
    if (col) return Number(col.getAttribute('data-device-col'))
  }
  return -1
}

function stopDragListen() {
  stopAutoScroll()
  deviceHover.value = -1
  ringTip.value = null
  if (!dragCleanup) return
  dragCleanup()
  dragCleanup = null
}

function onStageScroll() {
  measure()
  if (deviceDrag.value) deviceDrag.value = { ...deviceDrag.value }
}

function onDragWheel(e) {
  if (!deviceDrag.value || !scroller.value) return
  scroller.value.scrollTop += e.deltaY
  scroller.value.scrollLeft += e.deltaX
  measure()
  deviceDrag.value = { ...deviceDrag.value }
}

function onDeviceMove(e) {
  const d = deviceDrag.value
  if (!d) return
  lastPointer = { x: e.clientX, y: e.clientY }
  const dx = e.clientX - d.originX
  const dy = e.clientY - d.originY
  const moved = d.moved || dx * dx + dy * dy > 16
  deviceHover.value = columnUnderPoint(e.clientX, e.clientY)
  deviceDrag.value = { ...d, x: e.clientX, y: e.clientY, moved }
  if (!scrollRaf) scrollRaf = requestAnimationFrame(autoScrollStep)
}

function onDeviceUp(e) {
  const d = deviceDrag.value
  const to = columnUnderPoint(e.clientX, e.clientY)
  const moved = !!d?.moved
  stopDragListen()
  deviceDrag.value = null
  if (!d || !moved) return
  if (to < 0 || to === d.fromIndex) return
  if (!columns.value[to] || !deviceAt(to) || !deviceAt(d.fromIndex)) return
  openCreate(d.fromIndex, to)
}

function startDeviceDrag(e, index) {
  if (!props.canWrite || !deviceAt(index)) return
  e.preventDefault()
  deviceDrag.value = {
    fromIndex: index,
    x: e.clientX,
    y: e.clientY,
    originX: e.clientX,
    originY: e.clientY,
    moved: false,
  }
  lastPointer = { x: e.clientX, y: e.clientY }
  window.addEventListener('pointermove', onDeviceMove)
  window.addEventListener('pointerup', onDeviceUp)
  window.addEventListener('wheel', onDragWheel, { passive: true })
  dragCleanup = () => {
    window.removeEventListener('pointermove', onDeviceMove)
    window.removeEventListener('pointerup', onDeviceUp)
    window.removeEventListener('wheel', onDragWheel)
  }
}

function openCreate(fromIndex, toIndex) {
  createFrom.value = columns.value[fromIndex]
  createTo.value = columns.value[toIndex]
  pickA.value = null
  pickB.value = null
  createOpen.value = true
}

function chooseIface(deviceId, iface) {
  if (iface.connection) return
  if (deviceId === createFrom.value) pickA.value = iface.id
  else pickB.value = iface.id
}

function pickedId(deviceId) {
  return deviceId === createFrom.value ? pickA.value : pickB.value
}

function selectCable(c) {
  selected.value = c
  syncEditFrom(c)
}

const cableGuardOpen = ref(false)
const cableForm = computed(() => ({
  label: editLabel.value,
  a: editA.value,
  b: editB.value,
}))
const { close: closeCable } = useFormGuard(cableForm, cableGuardOpen, () => {
  selected.value = null
})

watch(selected, async (cable) => {
  cableGuardOpen.value = false
  if (!cable) return
  await nextTick()
  cableGuardOpen.value = true
})

function closeSelected() {
  closeCable()
}

const createGuardOpen = ref(false)
const createForm = computed(() => ({ a: pickA.value, b: pickB.value }))
const { close: closeCreateGuard } = useFormGuard(createForm, createGuardOpen, () => {
  createOpen.value = false
})

watch(createOpen, async (open) => {
  createGuardOpen.value = false
  if (!open) return
  await nextTick()
  createGuardOpen.value = true
})

function closeCreate() {
  closeCreateGuard()
}

function saveCreate() {
  if (!props.canWrite || !pickA.value || !pickB.value) return
  saving.value = true
  createConnection({ interface_a_id: pickA.value, interface_b_id: pickB.value, label: '' })
    .then(() => {
      pickA.value = null
      pickB.value = null
      createGuardOpen.value = false
      createOpen.value = false
      return load()
    })
    .catch((err) =>
      toast.add({ color: 'error', title: 'Could not save cable', description: errMsg(err) }),
    )
    .finally(() => {
      saving.value = false
    })
}

function saveSelected() {
  if (!selected.value || !props.canWrite) return
  if (!editA.value || !editB.value) return
  saving.value = true
  updateConnection(selected.value.id, {
    interface_a_id: editA.value,
    interface_b_id: editB.value,
    label: editLabel.value,
  })
    .then(() => {
      selected.value = null
      return load()
    })
    .catch((err) =>
      toast.add({ color: 'error', title: 'Could not change cable', description: errMsg(err) }),
    )
    .finally(() => {
      saving.value = false
    })
}

function removeCable(cable) {
  if (!cable || !props.canWrite) return
  saving.value = true
  deleteConnection(cable.id)
    .then(() => {
      if (selected.value?.id === cable.id) selected.value = null
      return load()
    })
    .catch((err) =>
      toast.add({ color: 'error', title: 'Could not remove cable', description: errMsg(err) }),
    )
    .finally(() => {
      saving.value = false
    })
}

async function removeSelected() {
  const cable = selected.value
  if (!cable || !props.canWrite) return
  const name =
    (cable.label || '').trim() || `${cable.device_a_name || 'A'} – ${cable.device_b_name || 'B'}`
  if (!(await confirmDelete(`cable ${name}`))) return
  removeCable(cable)
}

function ifaceItems(deviceId) {
  const dev = byId.value[deviceId]
  return sortedIfaces(dev?.interfaces).map((i) => ({
    label: i.description ? `${i.name} — ${i.description}` : i.name,
    value: i.id,
  }))
}

const selectedWritable = computed(() => !!selected.value && props.canWrite)

onMounted(() => {
  load()
  window.addEventListener('resize', measure)
})
onBeforeUnmount(() => {
  window.removeEventListener('resize', measure)
  stopDragListen()
  stopAutoScroll()
})

watch(
  () => props.deviceIds.join(','),
  () => load(),
)

watch(drawnCables, () => nextTick(measure))
</script>

<template>
  <div class="flex min-h-0 flex-1 flex-col gap-3">
    <div class="flex flex-wrap items-center gap-3 shrink-0">
      <p class="text-muted text-sm m-0">
        Connected physical interfaces only. Point at a port circle to see where it connects, and
        click it to open that device in the next column. Drag from one device onto another to
        create a cable, then pick one interface on each side and save. Click a cable line to change
        or remove it.
      </p>
      <UButton
        label="Add device"
        icon="i-lucide-plus"
        size="sm"
        color="neutral"
        variant="outline"
        class="ml-auto shrink-0"
        @click="addColumn"
      />
    </div>
    <div v-if="error" class="text-error text-sm">{{ error }}</div>
    <div
      ref="scroller"
      data-pair-scroller
      class="flex min-h-0 flex-1 gap-3 overflow-auto"
      :class="deviceDrag ? 'select-none cursor-grabbing' : ''"
      @scroll="onStageScroll"
    >
      <div ref="stage" class="relative flex min-h-full w-max items-start gap-8 p-2">
        <div
          v-for="(deviceId, index) in columns"
          :key="index"
          class="flex w-44 shrink-0 flex-col gap-1 rounded-lg border border-default bg-default p-1.5"
          :class="columnClass(index)"
          :data-device-col="index"
        >
          <div class="flex items-center gap-0.5">
            <button
              v-if="canWrite && deviceAt(index)"
              type="button"
              title="Drag onto another device to connect"
              class="inline-flex size-6 shrink-0 cursor-grab touch-none items-center justify-center rounded text-muted hover:bg-elevated"
              data-device-grip
              @pointerdown="startDeviceDrag($event, index)"
            >
              <UIcon name="i-lucide-grip-vertical" class="size-4" />
            </button>
            <USelectMenu
              :model-value="deviceId || undefined"
              :items="deviceChoices(index)"
              value-key="id"
              label-key="name"
              placeholder="Device…"
              size="sm"
              class="min-w-0 flex-1"
              @update:model-value="(v) => setDevice(index, deviceIdFromPicker(v))"
            />
            <UButton
              v-if="columns.length > 1"
              icon="i-lucide-x"
              size="xs"
              color="neutral"
              variant="ghost"
              title="Remove column"
              @click="removeColumn(index)"
            />
          </div>
          <div v-if="loading && deviceId && !deviceAt(index)" class="text-xs text-muted">
            Loading…
          </div>
          <div v-else-if="deviceAt(index)" class="flex flex-col">
            <div
              v-for="iface in portsAt(index)"
              :key="iface.id"
              class="grid grid-cols-[1.25rem_minmax(0,1fr)_1.25rem] items-center gap-1 rounded px-0.5 py-0.5 hover:bg-elevated/60"
            >
              <button
                v-if="showHandle(iface, index, 'left')"
                type="button"
                :class="handleClass(iface)"
                :data-port-handle="true"
                :data-port-key="portKey(index, iface.id, 'left')"
                :data-iface-id="iface.id"
                :data-col="index"
                data-dir="left"
                @pointerenter="showRingTip($event, iface)"
                @pointermove="showRingTip($event, iface)"
                @pointerleave="ringTip = null"
                @click="follow(index, iface)"
              />
              <span v-else class="size-5" />
              <div class="min-w-0 leading-tight text-center">
                <div class="truncate font-mono text-xs">{{ iface.name }}</div>
                <div v-if="iface.description" class="truncate text-[10px] text-muted">
                  {{ iface.description }}
                </div>
              </div>
              <button
                v-if="showHandle(iface, index, 'right')"
                type="button"
                :class="handleClass(iface) + ' justify-self-end'"
                :data-port-handle="true"
                :data-port-key="portKey(index, iface.id, 'right')"
                :data-iface-id="iface.id"
                :data-col="index"
                data-dir="right"
                @pointerenter="showRingTip($event, iface)"
                @pointermove="showRingTip($event, iface)"
                @pointerleave="ringTip = null"
                @click="follow(index, iface)"
              />
              <span v-else class="size-5 justify-self-end" />
            </div>
            <div v-if="!portsAt(index).length" class="text-xs text-muted">No connections.</div>
          </div>
          <div v-else-if="!deviceId" class="text-xs text-muted">Select a device.</div>
        </div>
        <svg class="pointer-events-none absolute inset-0 z-0 h-full w-full overflow-visible">
          <path
            v-for="c in drawnCables"
            :key="'hit-' + c.id + '-' + c.leftIndex"
            :d="cablePath(c)"
            fill="none"
            stroke="transparent"
            stroke-width="16"
            class="cursor-pointer"
            style="pointer-events: stroke"
            @pointerdown.stop="selectCable(c)"
          />
          <path
            v-for="c in drawnCables"
            :key="c.id + '-' + c.leftIndex"
            :d="cablePath(c)"
            fill="none"
            :stroke="
              selected?.id === c.id
                ? 'var(--ui-success)'
                : c.source === 'netbox'
                  ? 'var(--ui-warning)'
                  : 'var(--ui-primary)'
            "
            :stroke-dasharray="c.source === 'netbox' ? '6 4' : undefined"
            stroke-width="2.5"
          />
          <path
            v-if="deviceDragPath"
            :d="deviceDragPath"
            fill="none"
            stroke="var(--ui-primary)"
            stroke-dasharray="4 4"
            stroke-width="2"
          />
        </svg>
      </div>
    </div>

    <div
      v-if="ringTip"
      class="pointer-events-none fixed z-50 max-w-64 rounded-md border border-default bg-default px-2 py-1 text-xs shadow-lg"
      :style="{ left: `${ringTip.x}px`, top: `${ringTip.y}px` }"
    >
      {{ ringTip.text }}
    </div>

    <UModal
      :open="!!selected"
      :dismissible="false"
      title="Cable"
      :ui="wideModal"
      @update:open="(v) => !v && closeSelected()"
    >
      <template v-if="selected" #body>
        <div class="flex flex-col gap-3 text-sm">
          <UBadge
            :color="selected.source === 'netbox' ? 'neutral' : 'success'"
            variant="subtle"
            class="w-fit"
          >
            {{ selected.source === 'netbox' ? 'NetBox' : 'Factum' }}
          </UBadge>
          <template v-if="selectedWritable">
            <UFormField label="Label">
              <UInput v-model="editLabel" class="w-full" data-cable-label />
            </UFormField>
            <UFormField :label="selected.device_a_name || 'Device A'">
              <USelectMenu
                v-model="editA"
                :items="ifaceItems(selected.device_a_id)"
                value-key="value"
                label-key="label"
                class="w-full"
              />
            </UFormField>
            <UFormField :label="selected.device_b_name || 'Device B'">
              <USelectMenu
                v-model="editB"
                :items="ifaceItems(selected.device_b_id)"
                value-key="value"
                label-key="label"
                class="w-full"
              />
            </UFormField>
          </template>
          <p v-else class="text-muted">You need write permission to change this cable.</p>
        </div>
      </template>
      <template #footer>
        <div class="flex w-full gap-2">
          <UButton
            v-if="selectedWritable"
            label="Delete"
            icon="i-lucide-trash"
            color="error"
            variant="ghost"
            :loading="saving"
            @click="removeSelected"
          />
          <UButton
            :label="selectedWritable ? 'Cancel' : 'Close'"
            variant="ghost"
            class="ms-auto"
            @click="closeSelected"
          />
          <UButton v-if="selectedWritable" label="Save" :loading="saving" @click="saveSelected" />
        </div>
      </template>
    </UModal>

    <UModal
      :open="createOpen"
      :dismissible="false"
      title="New cable"
      :ui="wideModal"
      @update:open="(v) => !v && closeCreate()"
    >
      <template #body>
        <p class="mb-3 text-sm text-muted">
          Pick one free interface on each device. Connected interfaces cannot take another cable.
        </p>
        <div class="grid gap-4 sm:grid-cols-2">
          <div v-for="id in [createFrom, createTo]" :key="id" class="min-w-0">
            <div class="mb-2 truncate text-sm font-medium">{{ deviceName(id) }}</div>
            <div class="flex max-h-80 flex-col gap-1 overflow-auto">
              <button
                v-for="iface in dialogPorts(id)"
                :key="iface.id"
                type="button"
                class="rounded border px-2 py-1 text-left"
                :disabled="!!iface.connection"
                :class="
                  iface.connection
                    ? 'cursor-not-allowed border-transparent opacity-60'
                    : pickedId(id) === iface.id
                      ? 'border-primary bg-primary/10'
                      : 'border-transparent hover:bg-elevated'
                "
                @click="chooseIface(id, iface)"
              >
                <div class="font-mono text-xs">{{ iface.name }}</div>
                <div v-if="iface.description" class="truncate text-[11px] text-muted">
                  {{ iface.description }}
                </div>
                <div v-if="iface.connection" class="truncate text-[11px] text-muted">
                  Connected to {{ iface.connection.peer_device_name }}
                  {{ iface.connection.peer_interface_name }}
                </div>
              </button>
              <div v-if="!dialogPorts(id).length" class="text-xs text-muted">
                No physical interfaces.
              </div>
            </div>
          </div>
        </div>
      </template>
      <template #footer>
        <div class="flex w-full gap-2">
          <UButton label="Cancel" variant="ghost" class="ms-auto" @click="closeCreate" />
          <UButton
            label="Save"
            :loading="saving"
            :disabled="!pickA || !pickB"
            @click="saveCreate"
          />
        </div>
      </template>
    </UModal>
  </div>
</template>
