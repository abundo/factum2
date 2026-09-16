<script setup>
import { useToast } from '@nuxt/ui/composables'
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { Wunderbaum } from 'wunderbaum'
import { getForest, updateNamespace, updatePrefix, updateVrf } from '@/api/ipam'
import IpamPrefixForm from '@/components/IpamPrefixForm.vue'
import { useAuthStore } from '@/stores/auth'
import 'wunderbaum/dist/wunderbaum.css'
import '@/assets/wunderbaum-theme.css'

const authStore = useAuthStore()
const toast = useToast()

const props = defineProps({
  reloadKey: { type: Number, default: 0 },
})
const emit = defineEmits(['contextmenu', 'select'])

const TREE_WIDTH_KEY = 'factum:ipam-tree-width'
const TREE_WIDTH_DEFAULT = 800
const TREE_WIDTH_MIN = 320
const COL_WIDTHS_KEY = 'factum:ipam-tree-col-widths'
function loadTreeWidth() {
  const n = Number(localStorage.getItem(TREE_WIDTH_KEY))
  return Number.isFinite(n) && n >= TREE_WIDTH_MIN ? n : TREE_WIDTH_DEFAULT
}
function loadColWidths() {
  try {
    const raw = JSON.parse(localStorage.getItem(COL_WIDTHS_KEY) || 'null')
    return raw && typeof raw === 'object' ? raw : {}
  } catch {
    return {}
  }
}

const el = ref(null)
const selected = ref(null)
const form = ref({})
const saving = ref(false)
const treeWidth = ref(loadTreeWidth())
const treeResizing = ref(false)
let tree
let onContextMenu
let resizeMove
let resizeUp
let persistColsTimer

const selectedKind = computed(() => selected.value?.kind || selected.value?.type || '')
const isAllocated = computed(() => selectedKind.value === 'allocated')
const isVrf = computed(() => selectedKind.value === 'vrf')
const isNamedRow = computed(() => selectedKind.value === 'namespace' || isVrf.value)
const isNetboxVrf = computed(() => isVrf.value && selected.value?.source === 'netbox')
const vrfFieldsDisabled = computed(() => !authStore.canWrite || isNetboxVrf.value)
const canSaveSelected = computed(
  () => authStore.canWrite && (isAllocated.value || isNamedRow.value) && !isNetboxVrf.value,
)

function sourceLabel(source) {
  if (source === 'netbox') return 'NetBox'
  if (source === 'factum') return 'Factum'
  return source || ''
}

function sourceBadgeColor(source) {
  if (source === 'factum') return 'success'
  if (source === 'netbox') return 'neutral'
  return 'neutral'
}

function splitHextets(s) {
  if (!s) return []
  const parts = s.split(':')
  const out = []
  for (const p of parts) {
    if (!/^[0-9a-f]{1,4}$/i.test(p)) return null
    out.push(parseInt(p, 16))
  }
  return out
}

function parseIpv6(addr) {
  const lower = addr.toLowerCase()
  if (lower.includes('.')) return null
  const dbl = lower.indexOf('::')
  if (dbl >= 0) {
    if (lower.indexOf('::', dbl + 2) !== -1) return null
    const left = splitHextets(lower.slice(0, dbl))
    const right = splitHextets(lower.slice(dbl + 2))
    if (!left || !right) return null
    const missing = 8 - left.length - right.length
    if (missing < 0) return null
    return [...left, ...Array(missing).fill(0), ...right]
  }
  const parts = splitHextets(lower)
  if (!parts || parts.length !== 8) return null
  return parts
}

// Parse "10.1.0.0/16" / "2001:db8::/32" into a comparable key. Null if the
// title is not a CIDR (namespace / VRF rows).
function parseCidr(s) {
  if (!s || typeof s !== 'string') return null
  const slash = s.lastIndexOf('/')
  if (slash < 0) return null
  const addr = s.slice(0, slash).trim()
  const bits = Number(s.slice(slash + 1))
  if (!Number.isInteger(bits) || bits < 0) return null
  if (addr.includes('.')) {
    const parts = addr.split('.')
    if (parts.length !== 4 || bits > 32) return null
    let value = 0n
    for (const part of parts) {
      if (!/^\d{1,3}$/.test(part)) return null
      const n = Number(part)
      if (n > 255) return null
      value = (value << 8n) + BigInt(n)
    }
    return { family: 4, bits, value }
  }
  const hextets = parseIpv6(addr)
  if (!hextets || bits > 128) return null
  let value = 0n
  for (const h of hextets) value = (value << 16n) + BigInt(h)
  return { family: 6, bits, value }
}

function isPrefixNode(n) {
  const kind = n.kind || n.type
  return kind === 'allocated' || kind === 'pool'
}

// Longest-prefix-match order: IPv4 before IPv6, then network address,
// then longer prefix first (more specific wins on the same network).
function compareLpm(a, b) {
  if (a.family !== b.family) return a.family - b.family
  if (a.value < b.value) return -1
  if (a.value > b.value) return 1
  return b.bits - a.bits
}

function compareTreeNodes(a, b) {
  const aPfx = isPrefixNode(a)
  const bPfx = isPrefixNode(b)
  if (aPfx && bPfx) {
    const ap = parseCidr(a.title)
    const bp = parseCidr(b.title)
    if (ap && bp) return compareLpm(ap, bp)
    if (ap) return -1
    if (bp) return 1
    return (a.title || '').localeCompare(b.title || '')
  }
  if (aPfx !== bPfx) return aPfx ? -1 : 1
  return (a.title || '').localeCompare(b.title || '', undefined, { sensitivity: 'base' })
}

function sortPrefixNodes(nodes) {
  if (!Array.isArray(nodes) || nodes.length < 2) return nodes ?? []
  return nodes.slice().sort(compareTreeNodes)
}

function toWbNode(n) {
  const node = {
    key: n.key,
    title: n.title,
    lazy: !!n.lazy,
    expanded: !!n.expanded,
    type: n.type,
    ...n.data,
  }
  if (Array.isArray(n.children) && n.children.length) {
    node.children = sortPrefixNodes(n.children.map(toWbNode))
  } else if (!n.lazy) {
    node.children = []
  }
  return node
}

function toWbForest(rows) {
  return sortPrefixNodes((rows ?? []).map(toWbNode))
}

function selectedPayload(node) {
  if (!node) return null
  return {
    key: node.key,
    title: node.title,
    type: node.type,
    ...node.data,
  }
}

function fillForm(node) {
  const kind = node?.kind || node?.type
  if (kind === 'allocated') {
    form.value = {
      id: node.prefix_id || node.id,
      namespace_id: node.namespace_id,
      prefix: node.title,
      description: node.description ?? '',
      vrf_id: node.vrf_id || 0,
      dhcp_enabled: !!node.dhcp_enabled,
      dhcp_range_start: node.dhcp_range_start ?? '',
      dhcp_range_end: node.dhcp_range_end ?? '',
      dhcp_gateway: node.dhcp_gateway ?? '',
      dhcp_dns_servers: node.dhcp_dns_servers ?? '',
    }
    return
  }
  if (kind === 'namespace' || kind === 'vrf') {
    form.value = {
      id: node.id,
      namespace_id: node.namespace_id,
      name: node.title ?? '',
      description: node.description ?? '',
      rd: node.rd ?? '',
      import_rt: node.import_rt ?? '',
      export_rt: node.export_rt ?? '',
    }
    return
  }
  form.value = {}
}

function onActivate(node) {
  const payload = selectedPayload(node)
  selected.value = payload
  fillForm(payload)
  emit('select', payload)
}

function prefixPayload(f) {
  const payload = {
    prefix: (f.prefix ?? '').trim(),
    vrf_id: Number(f.vrf_id) || 0,
    description: f.description ?? '',
  }
  if (authStore.dhcpEnabled) {
    payload.dhcp_enabled = !!f.dhcp_enabled
    payload.dhcp_range_start = (f.dhcp_range_start ?? '').trim()
    payload.dhcp_range_end = (f.dhcp_range_end ?? '').trim()
    payload.dhcp_gateway = (f.dhcp_gateway ?? '').trim()
    payload.dhcp_dns_servers = f.dhcp_dns_servers ?? ''
  }
  return payload
}

function errMsg(err, fallback) {
  return err.response?.data?.error ?? fallback
}

function saveSelected() {
  if (!canSaveSelected.value) return
  const f = form.value
  const kind = selectedKind.value
  const revealKeys = selected.value?.key ? keyPath(selected.value.key) : []
  let req
  if (kind === 'allocated') {
    if (!f.id || !f.namespace_id) return
    req = updatePrefix(f.namespace_id, f.id, prefixPayload(f))
  } else if (kind === 'namespace') {
    const name = (f.name ?? '').trim()
    if (!f.id || !name) return
    req = updateNamespace(f.id, { name, description: f.description ?? '' })
  } else if (kind === 'vrf') {
    const name = (f.name ?? '').trim()
    if (!f.id || !f.namespace_id || !name) return
    req = updateVrf(f.namespace_id, f.id, {
      name,
      description: f.description ?? '',
      rd: f.rd ?? '',
      import_rt: f.import_rt ?? '',
      export_rt: f.export_rt ?? '',
    })
  } else {
    return
  }
  saving.value = true
  req
    .then(() => load(revealKeys))
    .catch((err) =>
      toast.add({ color: 'error', title: 'Error', description: errMsg(err, 'Request failed.') }),
    )
    .finally(() => {
      saving.value = false
    })
}

function startResize(event) {
  event.preventDefault()
  treeResizing.value = true
  const startX = event.clientX
  const startWidth = treeWidth.value
  const parent = event.currentTarget?.parentElement
  const max = Math.max(TREE_WIDTH_MIN, Math.floor((parent?.clientWidth ?? window.innerWidth) * 0.7))
  document.body.style.cursor = 'col-resize'
  function onMove(moveEvent) {
    const next = startWidth + (moveEvent.clientX - startX)
    treeWidth.value = Math.min(max, Math.max(TREE_WIDTH_MIN, next))
  }
  function onUp() {
    treeResizing.value = false
    document.body.style.cursor = ''
    window.removeEventListener('mousemove', onMove)
    window.removeEventListener('mouseup', onUp)
    resizeMove = null
    resizeUp = null
    localStorage.setItem(TREE_WIDTH_KEY, String(treeWidth.value))
  }
  resizeMove = onMove
  resizeUp = onUp
  window.addEventListener('mousemove', onMove)
  window.addEventListener('mouseup', onUp)
}

function renderCell(e) {
  for (const col of Object.values(e.renderColInfosById ?? {})) {
    const data = e.node.data ?? {}
    switch (col.id) {
      case 'kind':
        col.elem.textContent = kindLabel(data.kind || e.node.type)
        break
      case 'dhcp':
        col.elem.textContent = data.kind === 'allocated' && data.dhcp_enabled ? 'On' : ''
        break
      case 'description':
        col.elem.textContent = data.description || ''
        break
      default:
        break
    }
  }
}

function kindLabel(kind) {
  switch (kind) {
    case 'namespace':
      return 'Namespace'
    case 'pool':
      return 'Allowed'
    case 'vrf':
      return 'VRF'
    case 'allocated':
      return 'Prefix'
    default:
      return kind || ''
  }
}

function destroyTree() {
  if (el.value && onContextMenu) {
    el.value.removeEventListener('contextmenu', onContextMenu)
    onContextMenu = null
  }
  if (tree) {
    // destroy() does `element.outerHTML = element.outerHTML`, which
    // replaces the DOM node and leaves Vue's ref pointing at a detached
    // element. Only call it on unmount, when the host is going away.
    tree.resizeObserver?.disconnect()
    tree = null
  }
}

function bindContextMenu() {
  if (!el.value || onContextMenu) return
  onContextMenu = (ev) => {
    ev.preventDefault()
    const node = Wunderbaum.getNode(ev)
    if (node) node.setActive()
    emit('contextmenu', {
      x: ev.clientX,
      y: ev.clientY,
      node: node ? selectedPayload(node) : null,
    })
  }
  el.value.addEventListener('contextmenu', onContextMenu)
}

function persistColWidths() {
  if (!tree?.columns) return
  const widths = {}
  for (const col of tree.columns) {
    if (Number.isFinite(col.customWidthPx) && col.customWidthPx > 0) {
      widths[col.id] = Math.round(col.customWidthPx)
    }
  }
  const json = JSON.stringify(widths)
  if (json === '{}' && !localStorage.getItem(COL_WIDTHS_KEY)) return
  localStorage.setItem(COL_WIDTHS_KEY, json)
}

function schedulePersistColWidths() {
  clearTimeout(persistColsTimer)
  persistColsTimer = setTimeout(persistColWidths, 250)
}

function treeColumns() {
  const saved = loadColWidths()
  const cols = [
    { id: '*', title: 'Name', width: '320px', minWidth: '140px' },
    { id: 'kind', title: 'Kind', width: '110px', minWidth: '70px' },
  ]
  if (authStore.dhcpEnabled) {
    cols.push({ id: 'dhcp', title: 'DHCP', width: '70px', minWidth: '50px' })
  }
  cols.push({ id: 'description', title: 'Description', width: '*', minWidth: '80px' })
  for (const col of cols) {
    const n = Number(saved[col.id])
    if (Number.isFinite(n) && n > 0) col.customWidthPx = n
  }
  return cols
}

function buildTree(source) {
  if (!el.value) return
  tree = new Wunderbaum({
    element: el.value,
    id: 'ipam',
    header: true,
    debugLevel: 0,
    // Must match the painted row height. CSS-only --wb-row-outer-height
    // left the highlight 36px tall while rows were still placed 22px
    // apart, so the selection box spilled into the neighbours.
    rowHeightPx: 28,
    // Row selection only — cell/column nav paints a second highlight using
    // Wunderbaum's light defaults, which flash white in dark mode.
    navigationModeOption: 'row',
    // Built-in chevrons need Bootstrap Icons (not loaded). Use [+]/[−]
    // markup so expanders look like a classic tree control.
    iconMap: {
      ...Wunderbaum.iconMaps?.bootstrap,
      expanderExpanded: '<i class="wb-expander">−</i>',
      expanderCollapsed: '<i class="wb-expander">+</i>',
      expanderLazy: '<i class="wb-expander">+</i>',
    },
    source,
    columns: treeColumns(),
    columnsResizable: true,
    types: {
      namespace: { icon: false },
      pool: { icon: false },
      vrf: { icon: false },
      allocated: { icon: false },
    },
    lazyLoad: (e) => getForest(e.node.key).then((rows) => toWbForest(rows)),
    render: renderCell,
    update: schedulePersistColWidths,
    activate: (e) => {
      if (!e.node) return
      onActivate(e.node)
    },
  })
  bindContextMenu()
  expandFirstLevel()
}

function expandFirstLevel() {
  const nodes = tree?.root?.children ?? []
  for (const node of nodes) {
    if (!node.expanded) node.setExpanded(true)
  }
}

function keyPath(key) {
  const path = []
  let node = key ? tree?.findKey(key) : null
  while (node && node.parent) {
    path.unshift(node.key)
    if (node.parent.isRootNode()) break
    node = node.parent
  }
  return path
}

async function reveal(keys) {
  expandFirstLevel()
  for (const key of keys ?? []) {
    const node = tree?.findKey(key)
    if (node) await node.setExpanded(true)
  }
}

function load(revealKeys) {
  const keepKey = selected.value?.key
  return getForest()
    .then((rows) => {
      const source = toWbForest(rows)
      if (tree) {
        return tree.load(source)
      }
      buildTree(source)
    })
    .then(async () => {
      await reveal(revealKeys)
      if (!keepKey) return
      const node = tree?.findKey(keepKey)
      if (node) {
        node.setActive()
        return
      }
      selected.value = null
      fillForm(null)
    })
    .catch(() => {
      if (!tree) buildTree([])
    })
}

function filter(q) {
  if (!tree) return
  const s = (q ?? '').trim()
  if (!s) {
    tree.clearFilter()
    return
  }
  tree.filterNodes(s, { mode: 'hide' })
}

function expandAll() {
  tree?.expandAll(true, { loadLazy: true })
}

function collapseAll() {
  tree?.expandAll(false)
}

async function reloadNode(key) {
  if (!tree) {
    load()
    return
  }
  if (!key) {
    load()
    return
  }
  const node = tree.findKey(key)
  if (!node) {
    load()
    return
  }
  node.resetLazy()
  await node.setExpanded(true)
}

function expandNode(key) {
  const node = key ? tree?.findKey(key) : null
  if (node) node.setExpanded(true)
  else expandAll()
}

function collapseNode(key) {
  const node = key ? tree?.findKey(key) : null
  if (node) node.setExpanded(false)
  else collapseAll()
}

function selectKey(key) {
  const node = key ? tree?.findKey(key) : null
  if (node) node.setActive()
}

onMounted(load)
watch(
  () => props.reloadKey,
  () => {
    load()
  },
)
onBeforeUnmount(() => {
  clearTimeout(persistColsTimer)
  persistColWidths()
  destroyTree()
  if (resizeMove) window.removeEventListener('mousemove', resizeMove)
  if (resizeUp) window.removeEventListener('mouseup', resizeUp)
  document.body.style.cursor = ''
})

defineExpose({
  filter,
  expandAll,
  collapseAll,
  expandNode,
  collapseNode,
  reloadNode,
  reload: load,
  keyPath,
  selectKey,
})
</script>

<template>
  <div
    class="flex min-h-0 h-full flex-1 flex-col overflow-auto lg:flex-row lg:overflow-hidden"
    :class="{ 'select-none': treeResizing }"
  >
    <div
      class="flex min-h-80 min-w-0 flex-col overflow-hidden max-lg:!w-full lg:min-h-0 lg:shrink-0"
      :style="{ width: treeWidth + 'px' }"
    >
      <div ref="el" class="ipam-tree min-h-0 flex-1" />
    </div>
    <div
      class="hidden lg:block w-1 shrink-0 cursor-col-resize self-stretch hover:bg-primary/30"
      role="separator"
      aria-orientation="vertical"
      aria-label="Resize prefix tree"
      @mousedown="startResize"
    />
    <div
      class="mt-4 flex min-h-80 min-w-0 flex-1 flex-col overflow-hidden lg:mt-0 lg:min-h-0 lg:pl-3"
    >
      <div class="flex min-h-0 flex-1 flex-col gap-3 overflow-auto max-w-lg">
        <div v-if="!selected" class="text-muted-color text-sm">Select a row to see details.</div>
        <template v-else>
          <div>
            <h5 class="m-0">{{ isNamedRow ? form.name || selected.title : selected.title }}</h5>
            <div class="text-sm text-muted-color mt-1">{{ kindLabel(selectedKind) }}</div>
            <div v-if="isVrf && selected.source" class="mt-1">
              <UBadge
                :label="sourceLabel(selected.source)"
                :color="sourceBadgeColor(selected.source)"
                variant="subtle"
              />
            </div>
          </div>
          <IpamPrefixForm
            v-if="isAllocated"
            v-model="form"
            prefix-disabled
            :disabled="!authStore.canWrite"
            :show-vrf="!!selected.vrf_name"
            :vrf-name="selected.vrf_name || ''"
          />
          <template v-else-if="isNamedRow">
            <div>
              <label class="block font-bold mb-2">Name</label>
              <UInput v-model="form.name" :disabled="vrfFieldsDisabled" class="w-full" />
            </div>
            <div>
              <label class="block font-bold mb-2">Description</label>
              <UInput v-model="form.description" :disabled="vrfFieldsDisabled" class="w-full" />
            </div>
            <template v-if="isVrf">
              <div>
                <label class="block font-bold mb-2">RD</label>
                <UInput v-model="form.rd" :disabled="vrfFieldsDisabled" class="w-full" />
              </div>
              <div>
                <label class="block font-bold mb-2">Import route-target</label>
                <UInput v-model="form.import_rt" :disabled="vrfFieldsDisabled" class="w-full" />
              </div>
              <div>
                <label class="block font-bold mb-2">Export route-target</label>
                <UInput v-model="form.export_rt" :disabled="vrfFieldsDisabled" class="w-full" />
              </div>
            </template>
          </template>
          <template v-else>
            <div>
              <label class="block font-bold mb-2">Name</label>
              <UInput :model-value="selected.title" disabled class="w-full" />
            </div>
            <div v-if="selectedKind !== 'pool'">
              <label class="block font-bold mb-2">Description</label>
              <UInput :model-value="selected.description || ''" disabled class="w-full" />
            </div>
          </template>
          <div v-if="canSaveSelected" class="flex justify-end">
            <UButton label="Save" :loading="saving" @click="saveSelected" />
          </div>
        </template>
      </div>
    </div>
  </div>
</template>
