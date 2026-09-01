<script setup>
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { Wunderbaum } from 'wunderbaum'
import { getScopeTree } from '@/api/config'
import 'wunderbaum/dist/wunderbaum.css'
import '@/assets/wunderbaum-theme.css'

const props = defineProps({
  reloadKey: { type: Number, default: 0 },
})
const emit = defineEmits(['contextmenu', 'select'])

const el = ref(null)
let tree
let onContextMenu

function toWbNode(n) {
  const node = {
    key: n.key,
    title: n.title,
    expanded: false,
    type: n.type,
    ...n.data,
  }
  if (Array.isArray(n.children) && n.children.length) {
    node.children = n.children.map(toWbNode)
  } else {
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
    if (col.id === 'kind') {
      col.elem.textContent = kindLabel(data.kind || e.node.type)
    }
  }
}

function kindLabel(kind) {
  switch (kind) {
    case 'folder':
      return 'Folder'
    case 'site':
      return 'Site'
    case 'location':
      return 'Location'
    case 'device':
      return 'Device'
    case 'interface':
      return 'Interface'
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
    id: 'config-scopes',
    header: true,
    debugLevel: 0,
    rowHeightPx: 28,
    navigationModeOption: 'row',
    iconMap: {
      ...Wunderbaum.iconMaps?.bootstrap,
      expanderExpanded: '<i class="wb-expander">−</i>',
      expanderCollapsed: '<i class="wb-expander">+</i>',
      expanderLazy: '<i class="wb-expander">+</i>',
    },
    source,
    columns: [
      { id: '*', title: 'Name', width: '320px' },
      { id: 'kind', title: 'Kind', width: '*' },
    ],
    types: {
      folder: { icon: false },
      site: { icon: false },
      location: { icon: false },
      device: { icon: false },
      interface: { icon: false },
    },
    render: renderCell,
    activate: (e) => emit('select', selectedPayload(e.node)),
  })
  bindContextMenu()
}

function expandFirstLevel() {
  const nodes = tree?.root?.children ?? []
  for (const node of nodes) {
    if (!node.expanded) node.setExpanded(true)
  }
}

// Expand currently visible collapsed folders only — not their descendants.
function expandOneLevel() {
  if (!tree) return
  const toExpand = []
  tree.visit((node) => {
    if (!node.isExpandable() || node.expanded) return
    toExpand.push(node)
    return 'skip'
  })
  for (const node of toExpand) {
    node.setExpanded(true)
  }
}

function load() {
  return getScopeTree()
    .then((rows) => {
      const source = (rows ?? []).map(toWbNode)
      if (tree) {
        return tree.load(source)
      }
      buildTree(source)
    })
    .then(() => expandFirstLevel())
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
  expandOneLevel()
}

function collapseAll() {
  tree?.expandAll(false)
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
  reload: load,
})
</script>

<template>
  <div ref="el" class="ipam-tree" />
</template>
