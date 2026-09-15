<script setup>
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { Wunderbaum } from 'wunderbaum'
import { getSiteTree } from '@/api/sites'
import 'wunderbaum/dist/wunderbaum.css'
import '@/assets/wunderbaum-theme.css'

const props = defineProps({
  reloadKey: { type: Number, default: 0 },
})
const emit = defineEmits(['contextmenu', 'select'])

const el = ref(null)
let tree
let onContextMenu

function toWbNode(n, expandedKeys) {
  const node = {
    key: n.key,
    title: n.title,
    expanded: expandedKeys.has(n.key),
    type: n.type || 'site',
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
  if (!tree) return expandedKeys
  tree.visit((node) => {
    if (node.key && node.expanded) expandedKeys.add(node.key)
  })
  return expandedKeys
}

function selectedPayload(node) {
  if (!node) return null
  return {
    key: node.key,
    title: node.title,
    ...node.data,
  }
}

function sourceLabel(source) {
  if (source === 'netbox') return 'NetBox'
  if (source === 'factum') return 'Factum'
  return source || ''
}

function renderCell(e) {
  for (const col of Object.values(e.renderColInfosById ?? {})) {
    const data = e.node.data ?? {}
    if (col.id === 'source') {
      col.elem.textContent = sourceLabel(data.source)
    }
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
    if (node) node.setActive()
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
    id: 'sites',
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
      { id: '*', title: 'Name', width: '*' },
      { id: 'source', title: 'Source', width: '110px' },
    ],
    types: {
      site: { icon: false },
    },
    render: renderCell,
    activate: (e) => {
      emit('select', selectedPayload(e.node))
    },
  })
  bindContextMenu()
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
  for (const key of keys ?? []) {
    const node = tree?.findKey(key)
    if (node) await node.setExpanded(true)
  }
}

function load(revealKeys) {
  const keepKey = tree?.getActiveNode()?.key
  const expandedKeys = collectState()
  return getSiteTree()
    .then((rows) => {
      const source = (rows ?? []).map((n) => toWbNode(n, expandedKeys))
      if (tree) {
        return tree.load(source)
      }
      buildTree(source)
    })
    .then(async () => {
      await reveal(revealKeys)
      const selectKey = keepKey
      if (!selectKey) return
      const node = tree?.findKey(selectKey)
      if (node) node.setActive()
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
  tree?.expandAll(true)
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
onBeforeUnmount(destroyTree)

defineExpose({
  filter,
  expandAll,
  collapseAll,
  expandNode,
  collapseNode,
  reload: load,
  keyPath,
  selectKey,
})
</script>

<template>
  <div ref="el" class="ipam-tree min-h-0 h-full w-full flex-1" />
</template>
