<script setup>
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useToast } from '@nuxt/ui/composables'
import { Wunderbaum } from 'wunderbaum'
import { createSite, getSiteTree } from '@/api/sites'
import FormModal from '@/components/FormModal.vue'
import SearchInput from '@/components/SearchInput.vue'
import { useAuthStore } from '@/stores/auth'
import 'wunderbaum/dist/wunderbaum.css'
import '@/assets/wunderbaum-theme.css'

const props = defineProps({
  visible: { type: Boolean, default: false },
  selectedName: { type: String, default: '' },
})
const emit = defineEmits(['update:visible', 'select'])

const toast = useToast()
const authStore = useAuthStore()
const canWrite = computed(() => authStore.canWrite)

const el = ref(null)
const error = ref(null)
const selected = ref(null)
const filter = ref('')
const creating = ref(false)
const saving = ref(false)
const treeRev = ref(0)
const createForm = ref(emptyCreateForm())
let tree

function emptyCreateForm() {
  return { name: '', parent_id: 0, latitude: '', longitude: '' }
}

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
    if (el.value) el.value.replaceChildren()
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

function parentItems() {
  const items = [{ label: '(root)', value: 0 }]
  if (!tree) return items
  tree.visit((node) => {
    const id = node.data?.id
    if (!id) return
    items.push({ label: node.title || node.data?.name || String(id), value: id })
  })
  return items
}

const createParentItems = computed(() => {
  treeRev.value
  return parentItems()
})

function parseCoord(v) {
  if (v === '' || v == null) return 0
  const n = Number(v)
  return Number.isFinite(n) ? n : 0
}

function openCreate() {
  const node = tree?.getActiveNode()
  createForm.value = {
    name: '',
    parent_id: node?.data?.id || 0,
    latitude: '',
    longitude: '',
  }
  creating.value = true
}

function saveCreate() {
  const name = (createForm.value.name ?? '').trim()
  if (!name) {
    toast.add({ color: 'error', title: 'Name is required' })
    return
  }
  const parent = createForm.value.parent_id
  const payload = {
    name,
    parent_id: !parent || parent === 0 ? null : Number(parent),
    latitude: parseCoord(createForm.value.latitude),
    longitude: parseCoord(createForm.value.longitude),
  }
  saving.value = true
  createSite(payload)
    .then((row) => {
      creating.value = false
      createForm.value = emptyCreateForm()
      emit('select', {
        id: row.id,
        name: row.name || '',
        source: row.source,
        parent_id: row.parent_id ?? null,
      })
      close()
    })
    .catch((err) => {
      toast.add({
        color: 'error',
        title: 'Error',
        description: err.response?.data?.error ?? 'Create failed.',
      })
    })
    .finally(() => {
      saving.value = false
    })
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
      destroyTree()
      buildTree((rows ?? []).map(toWbNode))
      treeRev.value += 1
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
      treeRev.value += 1
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
      <div class="flex w-full items-center justify-between gap-2">
        <UButton
          v-if="canWrite"
          label="New site"
          icon="i-lucide-plus"
          variant="outline"
          @click="openCreate"
        />
        <span v-else />
        <div class="flex gap-2">
          <UButton label="Cancel" icon="i-lucide-x" variant="ghost" @click="close" />
          <UButton label="Select" icon="i-lucide-check" :disabled="!selected" @click="confirmSelect" />
        </div>
      </div>
    </template>
  </UModal>

  <FormModal
    :open="creating"
    :source="createForm"
    title="New site"
    :ui="{
      overlay: 'site-selector-create-layer',
      content: 'site-selector-create-layer',
    }"
    @update:open="
      (v) => {
        if (!v) creating = false
      }
    "
  >
    <template #body>
      <div class="flex flex-col gap-4">
        <UFormField label="Name">
          <UInput id="site-selector-create-name" v-model="createForm.name" class="w-full" autofocus />
        </UFormField>
        <UFormField label="Parent">
          <USelect v-model="createForm.parent_id" :items="createParentItems" class="w-full" />
        </UFormField>
        <UFormField label="Latitude">
          <UInput v-model="createForm.latitude" class="w-full" />
        </UFormField>
        <UFormField label="Longitude">
          <UInput v-model="createForm.longitude" class="w-full" />
        </UFormField>
      </div>
    </template>
    <template #footer>
      <UButton label="Cancel" variant="ghost" @click="creating = false" />
      <UButton label="Add" :loading="saving" @click="saveCreate" />
    </template>
  </FormModal>
</template>

<style>
.site-selector-layer {
  z-index: 200 !important;
}
.site-selector-create-layer {
  z-index: 210 !important;
}
</style>
