<script setup>
import { onBeforeUnmount, ref, watch } from 'vue'
import { Wunderbaum } from 'wunderbaum'
import { browseLdap } from '@/api/ldap'
import { ldapKindLabel, toLdapNode } from '@/utils/ldapTree'
import 'wunderbaum/dist/wunderbaum.css'
import '@/assets/wunderbaum-theme.css'

// Dialog that lets an admin navigate the configured LDAP/AD directory tree
// and pick a DN. selectable is "group" (the default, for role mappings) or
// "person" (for group membership). Callers listen for @select and assign
// the emitted DN.
const props = defineProps({
  visible: { type: Boolean, default: false },
  selectable: { type: String, default: 'group' },
})
const emit = defineEmits(['update:visible', 'select'])

const el = ref(null)
const error = ref(null)
const selected = ref(null)
let tree

function selectedPayload(node) {
  if (!node) return null
  return {
    key: node.key,
    title: node.title,
    type: node.type,
    ...node.data,
  }
}

function canSelect(node) {
  if (!node) return false
  if (props.selectable === 'person') return node.kind === 'person' || node.type === 'person'
  return !!node.is_group
}

function renderCell(e) {
  for (const col of Object.values(e.renderColInfosById ?? {})) {
    const data = e.node.data ?? {}
    if (col.id === 'kind') {
      col.elem.textContent = ldapKindLabel(data.kind || e.node.type)
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
      person: { icon: false },
      other: { icon: false },
    },
    lazyLoad: (e) =>
      browseLdap(e.node.data?.dn || e.node.key).then((rows) => (rows ?? []).map(toLdapNode)),
    render: renderCell,
    activate: (e) => {
      selected.value = selectedPayload(e.node)
    },
    dblclick: (e) => {
      if (canSelect(selectedPayload(e.node))) {
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
      buildTree((entries ?? []).map(toLdapNode))
    })
    .catch((err) => {
      if (el.value !== host) return
      error.value = err.response?.data?.error ?? 'Failed to browse the directory.'
      buildTree([])
    })
}

function selectAndClose(node) {
  const picked = selectedPayload(node)
  if (!canSelect(picked)) return
  emit('select', picked.key)
  close()
}

function confirmSelect() {
  if (!canSelect(selected.value)) return
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
    :dismissible="false"
    title="Browse directory"
    :ui="{
      overlay: 'ldap-browser-layer',
      content:
        'ldap-browser-layer w-[80vw] max-w-[80vw] h-[80vh] max-h-[80vh] sm:max-w-none flex flex-col bg-default',
      body: 'flex flex-1 min-h-0 flex-col overflow-hidden bg-default',
      header: 'bg-default',
      footer: 'bg-default',
    }"
    @update:open="(v) => emit('update:visible', v)"
  >
    <template #body>
      <UAlert v-if="error" color="error" variant="subtle" :title="error" class="mb-3 shrink-0" />
      <div v-if="visible" ref="el" class="ipam-tree min-h-0 flex-1" />
    </template>

    <template #footer>
      <UButton label="Cancel" icon="i-lucide-x" variant="ghost" @click="close" />
      <UButton
        label="Select"
        icon="i-lucide-check"
        :disabled="!canSelect(selected)"
        @click="confirmSelect"
      />
    </template>
  </UModal>
</template>

<style>
/* Form dialogs use the default modal layer. This browser opens on top of them. */
.ldap-browser-layer {
  z-index: 200 !important;
}
</style>
