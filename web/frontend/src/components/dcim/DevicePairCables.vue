<script setup>
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useToast } from '@nuxt/ui/composables'
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
const drag = ref(null)
const pending = ref(null)
const hoverKey = ref('')
const ringTip = ref(null)
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

function setDevice(index, id) {
  const next = columns.value.slice()
  next[index] = id || 0
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
  return sortedIfaces(deviceAt(index)?.interfaces).filter(isPhysical)
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

function isAdjacentCable(iface, index) {
  return !!cableSide(iface, index)
}

function portKey(index, ifaceId, dir) {
  return `${index}:${ifaceId}:${dir}`
}

function linking() {
  return drag.value || pending.value
}

function targetIndex(fromIndex, fromDir) {
  return fromDir === 'left' ? fromIndex - 1 : fromIndex + 1
}

function isValidDrop(iface, index) {
  const from = linking()
  if (!from) return false
  if (targetIndex(from.fromIndex, from.fromDir) !== index) return false
  if (iface.id === from.fromId && index === from.fromIndex) return false
  if (!iface.connection) return true
  return !!(from.reconnectId && iface.connection.id === from.reconnectId)
}

function handleClass(iface, index, dir) {
  const drop = isValidDrop(iface, index) && (!iface.connection || dir === cableSide(iface, index) || !iface.connection)
  const hot = drop && hoverKey.value === portKey(index, iface.id, dir)
  const mine = pending.value && pending.value.fromIndex === index && pending.value.fromId === iface.id
  const connectedHere = cableSide(iface, index) === dir
  const parts = [
    'relative z-10 size-5 shrink-0 rounded-full border-2 shadow-sm transition-transform touch-none',
  ]
  if (iface.connection) {
    parts.push(
      iface.connection.source === 'netbox'
        ? 'cursor-pointer border-warning bg-warning'
        : 'cursor-pointer border-primary bg-primary',
    )
  } else {
    parts.push(
      props.canWrite
        ? 'cursor-grab border-primary bg-default'
        : 'cursor-default border-primary bg-default',
    )
  }
  if (drop && !iface.connection) parts.push('scale-110 border-success bg-success')
  if (drop && connectedHere) parts.push('scale-110 border-success bg-success')
  if (mine && pending.value.fromDir === dir) parts.push('scale-110 ring-2 ring-primary')
  if (hot) parts.push('scale-125 ring-2 ring-success')
  return parts.join(' ')
}

function showHandle(iface, index, dir) {
  const side = cableSide(iface, index)
  if (iface.connection && side) return side === dir
  const hasPrev = index > 0 && !!columns.value[index - 1]
  const hasNext = !!columns.value[index + 1]
  if (iface.connection) return dir === 'right'
  if (dir === 'left') return hasPrev
  return hasNext || !hasPrev
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

const dragPath = computed(() => {
  const d = drag.value
  if (!d || !stage.value) return ''
  const box = stage.value.getBoundingClientRect()
  const from = points.value[portKey(d.fromIndex, d.fromId, d.fromDir)]
  if (!from) return ''
  const x = d.x - box.left
  const y = d.y - box.top
  const dx = Math.max(40, Math.abs(x - from.x) / 2)
  return `M ${from.x} ${from.y} C ${from.x + dx} ${from.y}, ${x - dx} ${y}, ${x} ${y}`
})

function findIface(index, id) {
  return (deviceAt(index)?.interfaces || []).find((p) => p.id === id) || null
}

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
    if ((oy === 'auto' || oy === 'scroll' || oy === 'overlay') && n.scrollHeight > n.clientHeight + 1) {
      out.push(n)
    }
    n = n.parentElement
  }
  return out
}

function autoScrollStep() {
  scrollRaf = 0
  if (!drag.value) return
  const y = lastPointer.y
  const edge = 64
  let moved = false
  const nodes = overflowAncestors(scroller.value || stage.value)
  for (const el of nodes) {
    const box = el.getBoundingClientRect()
    let dy = 0
    if (y < box.top + edge) {
      dy = -Math.max(4, Math.min(28, (box.top + edge - y) / 3))
    } else if (y > box.bottom - edge) {
      dy = Math.max(4, Math.min(28, (y - (box.bottom - edge)) / 3))
    }
    if (!dy) continue
    el.scrollTop += dy
    moved = true
  }
  if (!moved) return
  measure()
  if (drag.value) drag.value = { ...drag.value, x: lastPointer.x, y: lastPointer.y }
  scrollRaf = requestAnimationFrame(autoScrollStep)
}

function handleUnderPoint(x, y) {
  const stack = document.elementsFromPoint?.(x, y) ?? [document.elementFromPoint(x, y)]
  for (const el of stack) {
    const handle = el?.closest?.('[data-port-handle]')
    if (handle) return handle
  }
  return null
}

function stopDragListen() {
  stopAutoScroll()
  hoverKey.value = ''
  ringTip.value = null
  if (!dragCleanup) return
  dragCleanup()
  dragCleanup = null
}

function onStageScroll() {
  measure()
  if (drag.value) drag.value = { ...drag.value }
}

function onDragWheel(e) {
  if (!drag.value || !scroller.value) return
  scroller.value.scrollTop += e.deltaY
  measure()
  drag.value = { ...drag.value }
}

function onPointerMove(e) {
  if (!drag.value) return
  lastPointer = { x: e.clientX, y: e.clientY }
  const dx = e.clientX - drag.value.originX
  const dy = e.clientY - drag.value.originY
  const moved = drag.value.moved || dx * dx + dy * dy > 16
  if (moved) pending.value = null
  const handle = handleUnderPoint(e.clientX, e.clientY)
  hoverKey.value = handle?.getAttribute('data-port-key') || ''
  if (handle) {
    showRingTip(
      e,
      findIface(Number(handle.getAttribute('data-col')), Number(handle.getAttribute('data-iface-id'))),
    )
  } else {
    ringTip.value = null
  }
  drag.value = { ...drag.value, x: e.clientX, y: e.clientY, moved }
  if (!scrollRaf) scrollRaf = requestAnimationFrame(autoScrollStep)
}

function onPointerUp(e) {
  const d = drag.value
  const handle = handleUnderPoint(e.clientX, e.clientY)
  const toId = handle ? Number(handle.getAttribute('data-iface-id')) : 0
  const toIndex = handle ? Number(handle.getAttribute('data-col')) : -1
  const iface = toId ? findIface(toIndex, toId) : null
  // isValidDrop reads the in-progress drag, so decide before clearing it.
  const valid = !!(iface && isValidDrop(iface, toIndex))
  stopDragListen()
  drag.value = null
  if (!d) return
  if (!valid) {
    if (!d.moved) {
      const from = findIface(d.fromIndex, d.fromId)
      if (from?.connection) {
        follow(d.fromIndex, from)
        return
      }
      togglePending(d.fromId, d.fromIndex, d.fromDir)
    }
    return
  }
  connectPorts(d.fromId, toId, d.reconnectId)
}

function startDrag(e, iface, index, dir) {
  if (!showHandle(iface, index, dir)) return
  if (!props.canWrite) {
    if (iface.connection) follow(index, iface)
    return
  }
  if (iface.connection && !isAdjacentCable(iface, index)) {
    follow(index, iface)
    return
  }
  if (iface.connection && cableSide(iface, index) !== dir) return
  e.preventDefault()
  drag.value = {
    fromId: iface.id,
    fromIndex: index,
    fromDir: dir,
    x: e.clientX,
    y: e.clientY,
    originX: e.clientX,
    originY: e.clientY,
    moved: false,
    reconnectId: cableSide(iface, index) === dir ? iface.connection.id : null,
  }
  lastPointer = { x: e.clientX, y: e.clientY }
  window.addEventListener('pointermove', onPointerMove)
  window.addEventListener('pointerup', onPointerUp)
  window.addEventListener('wheel', onDragWheel, { passive: true })
  dragCleanup = () => {
    window.removeEventListener('pointermove', onPointerMove)
    window.removeEventListener('pointerup', onPointerUp)
    window.removeEventListener('wheel', onDragWheel)
  }
}

function togglePending(ifaceId, index, dir) {
  if (!props.canWrite) return
  if (!pending.value) {
    pending.value = { fromId: ifaceId, fromIndex: index, fromDir: dir, reconnectId: null }
    return
  }
  if (pending.value.fromId === ifaceId && pending.value.fromIndex === index) {
    pending.value = null
    return
  }
  if (targetIndex(pending.value.fromIndex, pending.value.fromDir) !== index) {
    pending.value = { fromId: ifaceId, fromIndex: index, fromDir: dir, reconnectId: null }
    return
  }
  const fromId = pending.value.fromId
  pending.value = null
  connectPorts(fromId, ifaceId, null)
}

function connectPorts(fromId, toId, reconnectId) {
  if (!props.canWrite) return
  const payload = { interface_a_id: fromId, interface_b_id: toId, label: '' }
  const req = reconnectId
    ? updateConnection(reconnectId, { ...payload, label: labelFor(reconnectId) })
    : createConnection(payload)
  req
    .then(() => load())
    .catch((err) => toast.add({ color: 'error', title: 'Could not save cable', description: errMsg(err) }))
}

function labelFor(id) {
  return findCable(id)?.label || ''
}

function selectCable(c) {
  selected.value = c
  syncEditFrom(c)
}

function closeSelected() {
  selected.value = null
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
    .catch((err) => toast.add({ color: 'error', title: 'Could not change cable', description: errMsg(err) }))
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
    .catch((err) => toast.add({ color: 'error', title: 'Could not remove cable', description: errMsg(err) }))
    .finally(() => {
      saving.value = false
    })
}

function removeSelected() {
  removeCable(selected.value)
}

function removeCableAt(index, iface) {
  if (!iface?.connection || !isAdjacentCable(iface, index)) return
  const cable = findCable(iface.connection.id)
  if (cable) removeCable(cable)
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
        Physical interfaces only. Point at a port circle to see where it connects, and click a
        connected circle to open that device in the next column. Drag between circles on
        neighbouring devices to create a cable, or click one free port then the other. Click a
        cable line to change it, or use the trash control to remove it.
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
      :class="drag ? 'select-none cursor-grabbing' : ''"
      @scroll="onStageScroll"
    >
      <div ref="stage" class="relative flex min-h-full w-max items-start gap-8 p-2">
        <div
          v-for="(deviceId, index) in columns"
          :key="index"
          class="flex w-44 shrink-0 flex-col gap-1 rounded-lg border border-default bg-default p-1.5"
        >
          <div class="flex items-center gap-0.5">
            <USelectMenu
              :model-value="deviceId || undefined"
              :items="deviceItems"
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
          <div v-if="loading && deviceId && !deviceAt(index)" class="text-xs text-muted">Loading…</div>
          <div v-else-if="deviceAt(index)" class="flex flex-col">
            <div
              v-for="iface in portsAt(index)"
              :key="iface.id"
              class="flex items-center gap-1 rounded px-0.5 py-0.5"
              :class="
                pending?.fromId === iface.id && pending?.fromIndex === index
                  ? 'bg-primary/10'
                  : 'hover:bg-elevated/60'
              "
            >
              <button
                v-if="showHandle(iface, index, 'left')"
                type="button"
                :class="handleClass(iface, index, 'left')"
                :data-port-handle="true"
                :data-port-key="portKey(index, iface.id, 'left')"
                :data-iface-id="iface.id"
                :data-col="index"
                data-dir="left"
                @pointerenter="showRingTip($event, iface)"
                @pointermove="showRingTip($event, iface)"
                @pointerleave="ringTip = null"
                @pointerdown="startDrag($event, iface, index, 'left')"
              />
              <UButton
                v-if="canWrite && iface.connection && cableSide(iface, index) === 'left'"
                icon="i-lucide-trash"
                variant="ghost"
                color="error"
                size="xs"
                :loading="saving"
                title="Remove cable"
                @pointerdown.stop
                @click.stop="removeCableAt(index, iface)"
              />
              <div class="min-w-0 flex-1 leading-tight">
                <div class="truncate font-mono text-xs">{{ iface.name }}</div>
                <div v-if="iface.description" class="truncate text-[10px] text-muted">
                  {{ iface.description }}
                </div>
              </div>
              <UButton
                v-if="canWrite && iface.connection && cableSide(iface, index) === 'right'"
                icon="i-lucide-trash"
                variant="ghost"
                color="error"
                size="xs"
                :loading="saving"
                title="Remove cable"
                @pointerdown.stop
                @click.stop="removeCableAt(index, iface)"
              />
              <button
                v-if="showHandle(iface, index, 'right')"
                type="button"
                :class="handleClass(iface, index, 'right')"
                :data-port-handle="true"
                :data-port-key="portKey(index, iface.id, 'right')"
                :data-iface-id="iface.id"
                :data-col="index"
                data-dir="right"
                @pointerenter="showRingTip($event, iface)"
                @pointermove="showRingTip($event, iface)"
                @pointerleave="ringTip = null"
                @pointerdown="startDrag($event, iface, index, 'right')"
              />
            </div>
            <div v-if="!portsAt(index).length" class="text-xs text-muted">No physical interfaces.</div>
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
            v-if="dragPath"
            :d="dragPath"
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
      title="Cable"
      :ui="{ content: 'sm:max-w-md' }"
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
      <template v-if="selectedWritable" #footer>
        <UButton label="Cancel" variant="ghost" @click="closeSelected" />
        <UButton
          label="Remove cable"
          color="error"
          variant="outline"
          :loading="saving"
          @click="removeSelected"
        />
        <UButton label="Save" :loading="saving" @click="saveSelected" />
      </template>
    </UModal>
  </div>
</template>
