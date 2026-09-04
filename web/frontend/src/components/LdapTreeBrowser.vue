<script setup>
import { onBeforeUnmount, ref, watch } from 'vue'
import { Wunderbaum } from 'wunderbaum'
import { browseLdap } from '@/api/ldap'
import 'wunderbaum/dist/wunderbaum.css'
import '@/assets/wunderbaum-theme.css'

// Dialog that lets an admin navigate the configured LDAP/AD directory tree
// and pick a group DN, instead of having to know/type it exactly. Generic
// over any dn-shaped field - callers just listen for @select and assign the
// emitted DN wherever it belongs, so this can be reused anywhere else a DN
// needs picking, not just the group -> role mapping dialog.
defineProps({
  visible: { type: Boolean, default: false },
})
const emit = defineEmits(['update:visible', 'select'])

const el = ref(null)
const error = ref(null)
const selected = ref(null)
let tree

function toWbNode(entry) {
  return {
    key: entry.dn,
    title: entry.name || entry.dn,
    lazy: !entry.is_group,
    type: entry.is_group ? 'group' : 'container',
    is_group: !!entry.is_group,
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

function kindLabel(kind) {
  switch (kind) {
    case 'group':
      return 'Group'
    case 'container':
      return 'Container'
    default:
      return kind || ''
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

function destroyTree() {
  if (tree) {
    // destroy() does `element.outerHTML = element.outerHTML`, which
    // replaces the DOM node and leaves Vue's ref pointing at a detached
    // element. Only disconnect here; the host is v-if'd with the modal.
    tree.resizeObserver?.disconnect()
    tree = null
  }
}

function buildTree(source) {
  if (!el.value) return
  tree = new Wunderbaum({
    element: el.value,
    id: 'ldap-browse',
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
      group: { icon: false },
      container: { icon: false },
    },
    lazyLoad: (e) => browseLdap(e.node.key).then((rows) => (rows ?? []).map(toWbNode)),
    render: renderCell,
    activate: (e) => {
      selected.value = selectedPayload(e.node)
    },
    dblclick: (e) => {
      if (e.node.data?.is_group) {
        selectAndClose(e.node)
        return false
      }
    },
  })
}

function loadRoot() {
  error.value = null
  selected.value = null
  const host = el.value
  browseLdap('')
    .then((entries) => {
      if (el.value !== host) return
      buildTree((entries ?? []).map(toWbNode))
    })
    .catch((err) => {
      if (el.value !== host) return
      error.value = err.response?.data?.error ?? 'Failed to browse the directory.'
      buildTree([])
    })
}

function selectAndClose(node) {
  if (!node?.data?.is_group) return
  emit('select', node.key)
  close()
}

function confirmSelect() {
  if (!selected.value?.is_group) return
  emit('select', selected.value.key)
  close()
}

function close() {
  emit('update:visible', false)
}

// Host is v-if'd with the modal. flush: 'post' so we run after the template
// ref is bound (default 'pre' misses that mount).
watch(
  el,
  (host) => {
    if (host) loadRoot()
    else destroyTree()
  },
  { flush: 'post' },
)

onBeforeUnmount(destroyTree)
</script>

<template>
  <UModal
    :open="visible"
    title="Browse directory"
    :ui="{ content: 'sm:max-w-2xl' }"
    @update:open="(v) => emit('update:visible', v)"
  >
    <template #body>
      <UAlert v-if="error" color="error" variant="subtle" :title="error" class="mb-3" />
      <div v-if="visible" class="h-80">
        <div ref="el" class="ipam-tree" />
      </div>
    </template>

    <template #footer>
      <UButton label="Cancel" icon="i-lucide-x" variant="ghost" @click="close" />
      <UButton
        label="Select"
        icon="i-lucide-check"
        :disabled="!selected?.is_group"
        @click="confirmSelect"
      />
    </template>
  </UModal>
</template>
