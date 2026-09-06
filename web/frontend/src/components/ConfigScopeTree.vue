<script setup>
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { Wunderbaum } from 'wunderbaum'
import { getScopeTree } from '@/api/config'
import 'wunderbaum/dist/wunderbaum.css'
import '@/assets/wunderbaum-theme.css'

const props = defineProps({
  reloadKey: { type: Number, default: 0 },
  canWrite: { type: Boolean, default: false },
})
const emit = defineEmits(['contextmenu', 'select', 'move'])

const el = ref(null)
let tree
let onContextMenu

function toWbNode(n, expandedKeys) {
  const node = {
    key: n.key,
    title: n.title,
    expanded: expandedKeys.has(n.key),
    type: n.type,
    ...n.data,
  }
  if (Array.isArray(n.children) && n.children.length) {
    node.children = n.children.map((c) => toWbNode(c, expandedKeys))
  } else {
    node.children = []
  }
  return node
}

function collectState() {
  const expandedKeys = new Set()
  const keys = new Set()
  let activeKey = null
  if (!tree) return { expandedKeys, keys, activeKey }
  tree.visit((node) => {
    if (!node.key) return
    keys.add(node.key)
    if (node.expanded) expandedKeys.add(node.key)
  })
  activeKey = tree.getActiveNode()?.key ?? null
  return { expandedKeys, keys, activeKey }
}

// A full reload (attach/delete) rebuilds every node as collapsed. Re-open
// parents of newly inserted nodes so the addition stays on screen.
function expandParentsOfNewNodes(nodes, previousKeys, expandedKeys, parentKey = null) {
  for (const n of nodes ?? []) {
    const isNew = previousKeys.size && !previousKeys.has(n.key)
    if (isNew && parentKey && previousKeys.has(parentKey)) {
      expandedKeys.add(parentKey)
    }
    if (Array.isArray(n.children) && n.children.length) {
      expandParentsOfNewNodes(n.children, previousKeys, expandedKeys, n.key)
    }
  }
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
      col.elem.textContent = kindLabel(data.kind || e.node.type, e.node.title)
    }
  }
}

function kindLabel(kind, name) {
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
    case 'parameter':
      return name === 'parameters' ? 'Parameters' : 'Parameter'
    case 'cli':
      return 'CLI'
    case 'service':
      return 'Service'
    case 'service_endpoint':
      return 'Endpoint'
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
      parameter: { icon: false },
      cli: { icon: false },
      service: { icon: false },
      service_endpoint: { icon: false },
    },
    render: renderCell,
    activate: (e) => emit('select', selectedPayload(e.node)),
    dnd: {
      dragStart: (e) => {
        if (!props.canWrite) return false
        const kind = e.node.data?.kind || e.node.type
        if (kind === 'interface') return false
        if (kind === 'folder' && e.node.title === 'global' && !e.node.data?.parent_id) return false
        return true
      },
      dragEnter: () => ['over', 'before', 'after'],
      drop: (e) => {
        const source = e.sourceNode
        const target = e.node
        if (!source?.data?.id || !target) return
        const mode = e.suggestedDropMode
        let parentId
        let sortOrder = null
        if (mode === 'appendChild') {
          parentId = target.data?.id
        } else {
          parentId = target.data?.parent_id
          if (!parentId) return
          const remaining = (target.parent?.children ?? []).filter((n) => n.key !== source.key)
          const idx = remaining.findIndex((n) => n.key === target.key)
          sortOrder = mode === 'before' ? idx : idx + 1
          if (idx < 0) sortOrder = remaining.length
        }
        if (!parentId || parentId === source.data.id) return
        emit('move', { id: source.data.id, parent_id: parentId, sort_order: sortOrder })
      },
    },
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
  const prev = collectState()
  return getScopeTree()
    .then((rows) => {
      const list = rows ?? []
      const expandedKeys = new Set(prev.expandedKeys)
      expandParentsOfNewNodes(list, prev.keys, expandedKeys)
      const source = list.map((n) => toWbNode(n, expandedKeys))
      if (tree) {
        return tree.load(source)
      }
      buildTree(source)
    })
    .then(() => {
      if (!prev.keys.size) expandFirstLevel()
      if (prev.activeKey) tree?.findKey(prev.activeKey)?.setActive()
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
