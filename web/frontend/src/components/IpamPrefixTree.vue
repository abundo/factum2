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
    node.children = n.children.map(toWbNode)
  } else if (!n.lazy) {
    node.children = []
  }
  return node
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
    lazyLoad: (e) => getForest(e.node.key).then((rows) => (rows ?? []).map(toWbNode)),
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
      const source = (rows ?? []).map(toWbNode)
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
