<script setup>
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { Wunderbaum } from 'wunderbaum'
import { getForest } from '@/api/ipam'
import 'wunderbaum/dist/wunderbaum.css'
import '@/assets/wunderbaum-theme.css'

const props = defineProps({
  reloadKey: { type: Number, default: 0 },
})
const emit = defineEmits(['contextmenu'])

const el = ref(null)
let tree
let onContextMenu

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
    ...node.data,
  }
}

function renderCell(e) {
  for (const col of Object.values(e.renderColInfosById ?? {})) {
    const data = e.node.data ?? {}
    switch (col.id) {
      case 'kind':
        col.elem.textContent = kindLabel(data.kind || e.node.type)
        break
      case 'vrf':
        col.elem.textContent = data.kind === 'allocated' ? data.vrf_name || '—' : '—'
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
    emit('contextmenu', {
      x: ev.clientX,
      y: ev.clientY,
      node: node ? selectedPayload(node) : null,
    })
  }
  el.value.addEventListener('contextmenu', onContextMenu)
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
    columns: [
      { id: '*', title: 'Name', width: '320px' },
      { id: 'kind', title: 'Kind', width: '110px' },
      { id: 'vrf', title: 'VRF', width: '120px' },
      { id: 'description', title: 'Description', width: '*' },
    ],
    types: {
      namespace: { icon: false },
      pool: { icon: false },
      vrf: { icon: false },
      allocated: { icon: false },
    },
    lazyLoad: (e) => getForest(e.node.key).then((rows) => toWbForest(rows)),
    render: renderCell,
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
  return getForest()
    .then((rows) => {
      const source = toWbForest(rows)
      if (tree) {
        return tree.load(source)
      }
      buildTree(source)
    })
    .then(() => reveal(revealKeys))
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

onMounted(load)
watch(
  () => props.reloadKey,
  () => {
    load()
  },
)
onBeforeUnmount(destroyTree)

defineExpose({
  filter,
  expandAll,
  collapseAll,
  expandNode,
  collapseNode,
  reloadNode,
  reload: load,
  keyPath,
})
</script>

<template>
  <div ref="el" class="ipam-tree" />
</template>
