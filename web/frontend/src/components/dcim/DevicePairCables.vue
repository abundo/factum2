<script setup>
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useToast } from '@nuxt/ui/composables'
import {
  createConnection,
  deleteConnection,
  getConnectionPair,
  updateConnection,
} from '@/api/connections'
import { useInterfaceTypes } from '@/composables/useInterfaceTypes'
import { compareInterfaceName } from '@/utils/interfaceNames'

const props = defineProps({
  deviceAId: { type: Number, default: 0 },
  deviceBId: { type: Number, default: 0 },
  canWrite: { type: Boolean, default: false },
})

const toast = useToast()
const { load: loadTypes, typeLabel } = useInterfaceTypes()

const stage = ref(null)
const scroller = ref(null)
const loading = ref(false)
const error = ref(null)
const pair = ref(null)
const filterA = ref('')
const filterB = ref('')
const selected = ref(null)
const saving = ref(false)
const editLabel = ref('')
const editA = ref(null)
const editB = ref(null)
const points = ref({})
const drag = ref(null)
const pending = ref(null)
const hoverId = ref(0)
let dragCleanup = null
let scrollRaf = 0
let lastPointer = { x: 0, y: 0 }

const cables = computed(() => pair.value?.cables ?? [])
const deviceA = computed(() => pair.value?.device_a ?? null)
const deviceB = computed(() => pair.value?.device_b ?? null)

function errMsg(err, fallback) {
  return err.response?.data?.error ?? fallback
}

function matchesFilter(iface, q) {
  if (!q) return true
  const hay = `${iface.name} ${iface.description || ''} ${iface.type || ''}`.toLowerCase()
  return hay.includes(q)
}

function sortedIfaces(list) {
  return [...(list ?? [])].sort((a, b) => compareInterfaceName(a.name, b.name) || a.id - b.id)
}

function visiblePorts(dev, q, otherId) {
  const list = sortedIfaces(dev?.interfaces)
  const query = q.trim().toLowerCase()
  if (!query) return list
  return list.filter(
    (p) => matchesFilter(p, query) || (p.connection && p.connection.peer_device_id === otherId),
  )
}

const portsA = computed(() => visiblePorts(deviceA.value, filterA.value, deviceB.value?.id))
const portsB = computed(() => visiblePorts(deviceB.value, filterB.value, deviceA.value?.id))

function isBetween(link) {
  if (!link || !deviceA.value || !deviceB.value) return false
  const ids = new Set([deviceA.value.id, deviceB.value.id])
  return ids.has(link.peer_device_id)
}

function linkingSide() {
  return drag.value?.fromSide || pending.value?.side || null
}

function isValidDrop(iface, side) {
  const from = linkingSide()
  if (!from || from === side) return false
  if (iface.id === drag.value?.fromId || iface.id === pending.value?.id) return false
  if (!iface.connection) return true
  if (!isBetween(iface.connection)) return false
  return !!(drag.value?.reconnectId && iface.connection.id === drag.value.reconnectId)
}

function handleClass(iface, side) {
  const drop = isValidDrop(iface, side)
  const hot = drop && hoverId.value === iface.id
  const mine = pending.value?.id === iface.id || drag.value?.fromId === iface.id
  const parts = [
    'relative z-10 size-5 shrink-0 rounded-full border-2 shadow-sm transition-transform touch-none',
  ]
  if (iface.connection && !isBetween(iface.connection)) {
    parts.push('cursor-not-allowed border-muted bg-muted')
  } else if (iface.connection && isBetween(iface.connection)) {
    parts.push(
      iface.connection.source === 'netbox'
        ? 'cursor-grab border-warning bg-warning'
        : 'cursor-grab border-primary bg-primary',
    )
  } else {
    parts.push(
      props.canWrite
        ? 'cursor-grab border-primary bg-default'
        : 'cursor-default border-primary bg-default',
    )
  }
  if (drop) parts.push('scale-110 border-success bg-success')
  if (mine) parts.push('scale-110 ring-2 ring-primary')
  if (hot) parts.push('scale-125 ring-2 ring-success')
  return parts.join(' ')
}

function load() {
  if (!props.deviceAId || !props.deviceBId) {
    pair.value = null
    selected.value = null
    error.value = null
    loading.value = false
    return Promise.resolve()
  }
  loading.value = true
  error.value = null
  return getConnectionPair(props.deviceAId, props.deviceBId)
    .then((data) => {
      pair.value = data
      if (selected.value) {
        const still = (data.cables || []).find((c) => c.id === selected.value.id)
        selected.value = still || null
        if (still) syncEditFrom(still)
      }
    })
    .catch((err) => {
      error.value = errMsg(err, 'Failed to load interfaces.')
      pair.value = null
    })
    .finally(() => {
      loading.value = false
      nextTick(measure)
    })
}

function syncEditFrom(cable) {
  editLabel.value = cable.label || ''
  const aOnLeft = cable.device_a_id === deviceA.value?.id
  editA.value = aOnLeft ? cable.interface_a_id : cable.interface_b_id
  editB.value = aOnLeft ? cable.interface_b_id : cable.interface_a_id
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
    const id = Number(el.getAttribute('data-iface-id'))
    const r = el.getBoundingClientRect()
    next[id] = { x: r.left + r.width / 2 - box.left, y: r.top + r.height / 2 - box.top }
  }
  points.value = next
}

function cablePath(c) {
  const aOnLeft = c.device_a_id === deviceA.value?.id
  const leftId = aOnLeft ? c.interface_a_id : c.interface_b_id
  const rightId = aOnLeft ? c.interface_b_id : c.interface_a_id
  const p1 = points.value[leftId]
  const p2 = points.value[rightId]
  if (!p1 || !p2) return ''
  const dx = Math.max(40, (p2.x - p1.x) / 2)
  return `M ${p1.x} ${p1.y} C ${p1.x + dx} ${p1.y}, ${p2.x - dx} ${p2.y}, ${p2.x} ${p2.y}`
}

const dragPath = computed(() => {
  const d = drag.value
  if (!d || !stage.value) return ''
  const box = stage.value.getBoundingClientRect()
  const from = points.value[d.fromId]
  if (!from) return ''
  const x = d.x - box.left
  const y = d.y - box.top
  const dx = Math.max(40, Math.abs(x - from.x) / 2)
  return `M ${from.x} ${from.y} C ${from.x + dx} ${from.y}, ${x - dx} ${y}, ${x} ${y}`
})

function findIface(id) {
  const lists = [deviceA.value?.interfaces, deviceB.value?.interfaces]
  for (const list of lists) {
    const hit = (list || []).find((p) => p.id === id)
    if (hit) return hit
  }
  return null
}

function cableById(id) {
  return cables.value.find((c) => c.id === id) || null
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
  const el = document.elementFromPoint(x, y)
  return el?.closest?.('[data-port-handle]') || null
}

function stopDragListen() {
  stopAutoScroll()
  hoverId.value = 0
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
  hoverId.value = handle ? Number(handle.getAttribute('data-iface-id')) : 0
  drag.value = { ...drag.value, x: e.clientX, y: e.clientY, moved }
  if (!scrollRaf) scrollRaf = requestAnimationFrame(autoScrollStep)
}

function onPointerUp(e) {
  const d = drag.value
  stopDragListen()
  drag.value = null
  if (!d) return
  const handle = handleUnderPoint(e.clientX, e.clientY)
  const toId = handle ? Number(handle.getAttribute('data-iface-id')) : 0
  const toSide = handle?.getAttribute('data-side')
  if (!toId || toId === d.fromId || toSide === d.fromSide) {
    if (!d.moved) {
      const iface = findIface(d.fromId)
      if (iface?.connection && isBetween(iface.connection)) {
        const cable = cableById(iface.connection.id)
        if (cable) selectCable(cable)
        return
      }
      togglePending(d.fromId, d.fromSide)
    }
    return
  }
  connectPorts(d.fromId, toId, d.reconnectId)
}

function startDrag(e, iface, side) {
  if (!props.canWrite) {
    if (iface.connection && isBetween(iface.connection)) {
      const cable = cableById(iface.connection.id)
      if (cable) selectCable(cable)
    }
    return
  }
  if (iface.connection && !isBetween(iface.connection)) return
  e.preventDefault()
  drag.value = {
    fromId: iface.id,
    fromSide: side,
    x: e.clientX,
    y: e.clientY,
    originX: e.clientX,
    originY: e.clientY,
    moved: false,
    reconnectId: isBetween(iface.connection) ? iface.connection.id : null,
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

function togglePending(ifaceId, side) {
  if (!props.canWrite) return
  if (!pending.value) {
    pending.value = { id: ifaceId, side }
    return
  }
  if (pending.value.id === ifaceId) {
    pending.value = null
    return
  }
  if (pending.value.side === side) {
    pending.value = { id: ifaceId, side }
    return
  }
  const fromId = pending.value.id
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
  return cables.value.find((c) => c.id === id)?.label || ''
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

function removeCableAt(iface) {
  if (!iface?.connection || !isBetween(iface.connection)) return
  const cable = cableById(iface.connection.id)
  if (cable) removeCable(cable)
}

const ifaceItemsA = computed(() =>
  sortedIfaces(deviceA.value?.interfaces).map((i) => ({
    label: i.description ? `${i.name} — ${i.description}` : i.name,
    value: i.id,
  })),
)
const ifaceItemsB = computed(() =>
  sortedIfaces(deviceB.value?.interfaces).map((i) => ({
    label: i.description ? `${i.name} — ${i.description}` : i.name,
    value: i.id,
  })),
)

const selectedWritable = computed(() => !!selected.value && props.canWrite)

function peerNote(iface) {
  if (!iface.connection || isBetween(iface.connection)) return ''
  return `${iface.connection.peer_device_name} ${iface.connection.peer_interface_name}`
}

onMounted(() => {
  loadTypes()
  load()
  window.addEventListener('resize', measure)
})
onBeforeUnmount(() => {
  window.removeEventListener('resize', measure)
  stopDragListen()
  stopAutoScroll()
})

watch(
  () => [props.deviceAId, props.deviceBId],
  () => load(),
)

watch(portsA, () => nextTick(measure))
watch(portsB, () => nextTick(measure))
</script>

<template>
  <div class="flex min-h-0 flex-1 flex-col gap-3">
    <p class="text-muted text-sm shrink-0">
      Drag between the port circles to create a cable, or click one port then the other. The list
      scrolls when you drag near the edge. Click a cable or a connected port to change it, or use
      the trash control to remove it. Cables between NetBox interfaces are also stored in NetBox.
    </p>
    <div v-if="error" class="text-error text-sm">{{ error }}</div>
    <div v-if="loading && deviceAId && deviceBId" class="text-muted text-sm">Loading…</div>
    <div
      v-if="deviceA && deviceB"
      ref="scroller"
      data-pair-scroller
      class="min-h-0 flex-1 overflow-auto rounded border border-default"
      :class="drag ? 'select-none cursor-grabbing' : ''"
      @scroll="onStageScroll"
    >
      <div ref="stage" class="relative grid min-h-full grid-cols-2 gap-24 p-3">
        <div class="min-w-0">
          <div class="mb-2 flex items-center justify-between gap-2">
            <div class="min-w-0">
              <div class="truncate font-medium">{{ deviceA.name }}</div>
              <div class="truncate text-xs text-muted">{{ deviceA.site }}</div>
            </div>
            <UInput v-model="filterA" placeholder="Filter ports" size="sm" class="w-36" />
          </div>
          <div class="flex flex-col gap-1">
            <div
              v-for="iface in portsA"
              :key="'a-' + iface.id"
              class="flex items-center gap-2 rounded px-1 py-1"
              :class="pending?.id === iface.id ? 'bg-primary/10' : 'hover:bg-elevated/50'"
            >
              <div class="min-w-0 flex-1">
                <div class="flex items-center gap-2">
                  <span class="font-mono text-sm">{{ iface.name }}</span>
                  <span class="truncate text-[11px] text-muted">{{ typeLabel(iface.type) }}</span>
                </div>
                <div class="truncate text-xs text-muted">
                  {{ iface.description || 'No description' }}
                  <span v-if="peerNote(iface)" class="ml-1">· {{ peerNote(iface) }}</span>
                </div>
              </div>
              <UButton
                v-if="canWrite && iface.connection && isBetween(iface.connection)"
                icon="i-lucide-trash"
                variant="ghost"
                color="error"
                size="xs"
                :loading="saving"
                title="Remove cable"
                @pointerdown.stop
                @click.stop="removeCableAt(iface)"
              />
              <button
                type="button"
                :class="handleClass(iface, 'a')"
                :data-port-handle="true"
                :data-iface-id="iface.id"
                data-side="a"
                :title="
                  iface.connection && isBetween(iface.connection)
                    ? 'Connected — drag to reconnect or click to edit'
                    : 'Drag to a circle on the other device'
                "
                @pointerdown="startDrag($event, iface, 'a')"
              />
            </div>
            <div v-if="!portsA.length" class="text-sm text-muted">No interfaces.</div>
          </div>
        </div>
        <div class="min-w-0">
          <div class="mb-2 flex items-center justify-between gap-2">
            <UInput v-model="filterB" placeholder="Filter ports" size="sm" class="w-36" />
            <div class="min-w-0 text-right">
              <div class="truncate font-medium">{{ deviceB.name }}</div>
              <div class="truncate text-xs text-muted">{{ deviceB.site }}</div>
            </div>
          </div>
          <div class="flex flex-col gap-1">
            <div
              v-for="iface in portsB"
              :key="'b-' + iface.id"
              class="flex items-center gap-2 rounded px-1 py-1"
              :class="pending?.id === iface.id ? 'bg-primary/10' : 'hover:bg-elevated/50'"
            >
              <button
                type="button"
                :class="handleClass(iface, 'b')"
                :data-port-handle="true"
                :data-iface-id="iface.id"
                data-side="b"
                :title="
                  iface.connection && isBetween(iface.connection)
                    ? 'Connected — drag to reconnect or click to edit'
                    : 'Drag to a circle on the other device'
                "
                @pointerdown="startDrag($event, iface, 'b')"
              />
              <UButton
                v-if="canWrite && iface.connection && isBetween(iface.connection)"
                icon="i-lucide-trash"
                variant="ghost"
                color="error"
                size="xs"
                :loading="saving"
                title="Remove cable"
                @pointerdown.stop
                @click.stop="removeCableAt(iface)"
              />
              <div class="min-w-0 flex-1">
                <div class="flex items-center gap-2">
                  <span class="font-mono text-sm">{{ iface.name }}</span>
                  <span class="truncate text-[11px] text-muted">{{ typeLabel(iface.type) }}</span>
                </div>
                <div class="truncate text-xs text-muted">
                  {{ iface.description || 'No description' }}
                  <span v-if="peerNote(iface)" class="ml-1">· {{ peerNote(iface) }}</span>
                </div>
              </div>
            </div>
            <div v-if="!portsB.length" class="text-sm text-muted">No interfaces.</div>
          </div>
        </div>
        <svg class="pointer-events-none absolute inset-0 z-0 h-full w-full overflow-visible">
          <path
            v-for="c in cables"
            :key="'hit-' + c.id"
            :d="cablePath(c)"
            fill="none"
            stroke="transparent"
            stroke-width="16"
            class="cursor-pointer"
            style="pointer-events: stroke"
            @pointerdown.stop="selectCable(c)"
          />
          <path
            v-for="c in cables"
            :key="c.id"
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
    <div v-else-if="!deviceAId || !deviceBId" class="text-muted text-sm">
      Select two devices to list their interfaces.
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
            <UFormField :label="deviceA?.name || 'Device A'">
              <USelectMenu
                v-model="editA"
                :items="ifaceItemsA"
                value-key="value"
                label-key="label"
                class="w-full"
              />
            </UFormField>
            <UFormField :label="deviceB?.name || 'Device B'">
              <USelectMenu
                v-model="editB"
                :items="ifaceItemsB"
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
