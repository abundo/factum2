<script setup>
import { onBeforeUnmount, ref, watch } from 'vue'
import { Wunderbaum } from 'wunderbaum'
import { getSiteTree } from '@/api/sites'
import SearchInput from '@/components/SearchInput.vue'
import 'wunderbaum/dist/wunderbaum.css'
import '@/assets/wunderbaum-theme.css'

const props = defineProps({
  visible: { type: Boolean, default: false },
  selectedName: { type: String, default: '' },
})
const emit = defineEmits(['update:visible', 'select'])

const el = ref(null)
const error = ref(null)
const selected = ref(null)
const filter = ref('')
let tree

function toWbNode(n) {
  const node = {
    key: n.key,
    title: n.title,
    type: n.type || 'site',
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
  if (tree) {
    tree.resizeObserver?.disconnect()
    tree = null
  }
}

function applyFilter(q) {
  if (!tree) return
  const s = (q ?? '').trim()
  if (!s) {
    tree.clearFilter()
    return
  }
  tree.filterNodes(s, { mode: 'hide' })
  tree.expandAll(true)
}

function findByName(name) {
  const want = (name ?? '').trim().toLowerCase()
  if (!want || !tree) return null
  let match = null
  tree.visit((node) => {
    if (match) return
    const title = (node.title || '').trim().toLowerCase()
    if (title === want) match = node
  })
  return match
}

function buildTree(source) {
  if (!el.value) return
  tree = new Wunderbaum({
    element: el.value,
    id: 'site-selector',
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
      selected.value = selectedPayload(e.node)
    },
    dblclick: (e) => {
      if (e.node) {
        selectAndClose(e.node)
        return false
      }
    },
  })
}

function loadTree() {
  error.value = null
  selected.value = null
  filter.value = ''
  const host = el.value
  getSiteTree()
    .then((rows) => {
      if (el.value !== host) return
      buildTree((rows ?? []).map(toWbNode))
      const roots = tree?.root?.children ?? []
      for (const node of roots) {
        if (!node.expanded) node.setExpanded(true)
      }
      const current = findByName(props.selectedName)
      if (current) current.setActive()
    })
    .catch((err) => {
      if (el.value !== host) return
      error.value = err.response?.data?.error ?? 'Failed to load sites.'
      buildTree([])
    })
}

function selectAndClose(node) {
  const payload = selectedPayload(node)
  if (!payload) return
  emit('select', {
    id: payload.id,
    name: payload.title || payload.name || '',
    source: payload.source,
    parent_id: payload.parent_id ?? null,
  })
  close()
}

function confirmSelect() {
  const node = tree?.getActiveNode()
  if (node) {
    selectAndClose(node)
    return
  }
  if (!selected.value) return
  emit('select', {
    id: selected.value.id,
    name: selected.value.title || selected.value.name || '',
    source: selected.value.source,
    parent_id: selected.value.parent_id ?? null,
  })
  close()
}

function close() {
  emit('update:visible', false)
}

watch(
  el,
  (host) => {
    if (host) loadTree()
    else destroyTree()
  },
  { flush: 'post' },
)

watch(filter, (q) => applyFilter(q))

onBeforeUnmount(destroyTree)
</script>

<template>
  <UModal
    :open="visible"
    title="Select site"
    :ui="{
      overlay: 'site-selector-layer',
      content:
        'site-selector-layer w-[80vw] max-w-[80vw] h-[80vh] max-h-[80vh] sm:max-w-none flex flex-col bg-default',
      body: 'flex flex-1 min-h-0 flex-col overflow-hidden bg-default',
      header: 'bg-default',
      footer: 'bg-default',
    }"
    @update:open="(v) => emit('update:visible', v)"
  >
    <template #body>
      <SearchInput v-model="filter" placeholder="Search sites..." class="mb-3 w-full shrink-0" />
      <UAlert v-if="error" color="error" variant="subtle" :title="error" class="mb-3 shrink-0" />
      <div v-if="visible" ref="el" class="ipam-tree min-h-0 flex-1" />
    </template>

    <template #footer>
      <UButton label="Cancel" icon="i-lucide-x" variant="ghost" @click="close" />
      <UButton label="Select" icon="i-lucide-check" :disabled="!selected" @click="confirmSelect" />
    </template>
  </UModal>
</template>

<style>
.site-selector-layer {
  z-index: 200 !important;
}
</style>
