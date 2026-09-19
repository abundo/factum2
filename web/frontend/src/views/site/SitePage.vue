<script setup>
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { useToast } from '@nuxt/ui/composables'
import { createSite, deleteSite, getSites, updateSite } from '@/api/sites'
import FormModal from '@/components/FormModal.vue'
import SearchInput from '@/components/SearchInput.vue'
import SiteTree from '@/components/SiteTree.vue'
import { useAuthStore } from '@/stores/auth'

defineOptions({ name: 'SitePage' })

const toast = useToast()
const router = useRouter()
const authStore = useAuthStore()
const treeRef = ref(null)
const filter = ref('')
const selected = ref(null)
const sites = ref([])
const form = ref(emptyForm())
const saving = ref(false)
const dialog = ref(null)
const confirm = ref(null)
const menu = ref({ open: false, x: 0, y: 0, node: null })

const canWrite = computed(() => authStore.canWrite)
const isLocal = computed(() => selected.value && selected.value.source !== 'netbox')
const canSave = computed(() => canWrite.value && isLocal.value)

const parentItems = computed(() => {
  const skip = new Set()
  if (selected.value?.id && dialog.value !== 'create') skip.add(selected.value.id)
  const items = [{ label: '(root)', value: 0 }]
  for (const s of sites.value) {
    if (skip.has(s.id)) continue
    items.push({ label: s.name, value: s.id })
  }
  return items
})

const menuItems = computed(() => itemsFor(menu.value.node))

function emptyForm() {
  return { name: '', parent_id: null, latitude: '', longitude: '' }
}

function errMsg(err, fallback) {
  return err.response?.data?.error ?? fallback
}

function applyFilter(q) {
  treeRef.value?.filter(q)
}

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

function itemsFor(node) {
  const items = []
  if (node) {
    items.push({ id: 'expand', label: 'Expand' }, { id: 'collapse', label: 'Collapse' })
  }
  if (!canWrite.value) return items
  if (items.length) items.push({ id: 'sep' })
  items.push({ id: 'add', label: node ? 'Add child site' : 'Add site' })
  if (node && node.source !== 'netbox') {
    items.push({ id: 'sep2' }, { id: 'del', label: 'Delete', danger: true })
  }
  return items
}

function onSelect(node) {
  selected.value = node
  fillForm(node)
}

function fillForm(node) {
  if (!node) {
    form.value = emptyForm()
    return
  }
  form.value = {
    name: node.title || node.name || '',
    parent_id: node.parent_id || 0,
    latitude: node.latitude || '',
    longitude: node.longitude || '',
  }
}

function loadSites() {
  return getSites()
    .then((data) => {
      sites.value = data ?? []
    })
    .catch(() => {
      sites.value = []
    })
}

function onContextMenu({ x, y, node }) {
  menu.value = { open: true, x, y, node }
}

function closeMenu() {
  menu.value = { ...menu.value, open: false }
}

function runMenu(id) {
  const node = menu.value.node
  closeMenu()
  if (id === 'expand') {
    treeRef.value?.expandNode(node?.key)
    return
  }
  if (id === 'collapse') {
    treeRef.value?.collapseNode(node?.key)
    return
  }
  if (id === 'add') {
    openCreate(node?.id ?? 0)
    return
  }
  if (id === 'del' && node) {
    confirm.value = { id: node.id, label: node.title }
  }
}

function openCreate(parentId = 0) {
  form.value = {
    name: '',
    parent_id: parentId || 0,
    latitude: '',
    longitude: '',
  }
  dialog.value = 'create'
}

function parseCoord(v) {
  if (v === '' || v == null) return 0
  const n = Number(v)
  return Number.isFinite(n) ? n : 0
}

function payloadFromForm() {
  const parent = form.value.parent_id
  return {
    name: (form.value.name ?? '').trim(),
    parent_id: !parent || parent === 0 ? null : Number(parent),
    latitude: parseCoord(form.value.latitude),
    longitude: parseCoord(form.value.longitude),
  }
}

function saveCreate() {
  const payload = payloadFromForm()
  if (!payload.name) {
    toast.add({ color: 'error', title: 'Name is required' })
    return
  }
  saving.value = true
  const revealKeys = payload.parent_id ? treeRef.value?.keyPath(String(payload.parent_id)) : []
  createSite(payload)
    .then(() => {
      dialog.value = null
      return Promise.all([treeRef.value?.reload(revealKeys), loadSites()])
    })
    .catch((err) =>
      toast.add({ color: 'error', title: 'Error', description: errMsg(err, 'Create failed.') }),
    )
    .finally(() => {
      saving.value = false
    })
}

function saveSelected() {
  if (!canSave.value || !selected.value?.id) return
  const payload = payloadFromForm()
  if (!payload.name) {
    toast.add({ color: 'error', title: 'Name is required' })
    return
  }
  saving.value = true
  const revealKeys = selected.value.key ? treeRef.value?.keyPath(selected.value.key) : []
  updateSite(selected.value.id, payload)
    .then(() => Promise.all([treeRef.value?.reload(revealKeys), loadSites()]))
    .catch((err) =>
      toast.add({ color: 'error', title: 'Error', description: errMsg(err, 'Save failed.') }),
    )
    .finally(() => {
      saving.value = false
    })
}

function performDelete() {
  const c = confirm.value
  if (!c) return
  saving.value = true
  deleteSite(c.id)
    .then(() => {
      confirm.value = null
      selected.value = null
      fillForm(null)
      return Promise.all([treeRef.value?.reload(), loadSites()])
    })
    .catch((err) =>
      toast.add({ color: 'error', title: 'Error', description: errMsg(err, 'Delete failed.') }),
    )
    .finally(() => {
      saving.value = false
    })
}

function onDocClick() {
  if (menu.value.open) closeMenu()
}

onMounted(() => {
  document.addEventListener('click', onDocClick)
  loadSites()
})
onBeforeUnmount(() => document.removeEventListener('click', onDocClick))
</script>

<template>
  <div class="card flex min-h-0 flex-1 flex-col overflow-hidden">
    <div class="flex flex-wrap gap-2 items-center justify-between mb-3 shrink-0">
      <h4 class="m-0">Sites</h4>
      <div class="flex flex-wrap gap-2 items-center">
        <SearchInput
          v-model="filter"
          placeholder="Filter..."
          class="w-56"
          @update:model-value="applyFilter"
        />
        <UButton
          icon="i-lucide-unfold-vertical"
          variant="outline"
          color="neutral"
          size="sm"
          title="Expand all"
          @click="treeRef?.expandAll()"
        />
        <UButton
          icon="i-lucide-fold-vertical"
          variant="outline"
          color="neutral"
          size="sm"
          title="Collapse all"
          @click="treeRef?.collapseAll()"
        />
        <UButton
          v-if="canWrite"
          icon="i-lucide-plus"
          size="sm"
          label="Site"
          @click="openCreate(0)"
        />
      </div>
    </div>
    <p class="text-muted-color text-sm mb-3 shrink-0">
      Regions, sites and locations synced from NetBox appear as one site tree. You can add Factum
      sites under any node. Right-click to add a child. NetBox-synced rows are read-only.
    </p>
    <div class="flex min-h-0 flex-1 flex-row overflow-hidden">
      <div class="flex shrink-0 flex-col overflow-hidden" style="width: 520px; min-width: 320px">
        <SiteTree ref="treeRef" class="h-full" @contextmenu="onContextMenu" @select="onSelect" />
      </div>
      <div class="flex min-w-64 min-h-0 flex-1 flex-col overflow-auto pl-6">
        <div class="flex min-h-0 flex-1 flex-col gap-3 overflow-auto max-w-lg">
          <div v-if="!selected" class="text-muted-color text-sm">Select a site to see details.</div>
          <template v-else>
            <div>
              <h5 class="m-0">{{ form.name || selected.title }}</h5>
              <div class="mt-1">
                <UBadge
                  :label="sourceLabel(selected.source)"
                  :color="sourceBadgeColor(selected.source)"
                  variant="subtle"
                />
              </div>
            </div>
            <UFormField label="Name">
              <UInput v-model="form.name" :disabled="!canSave" class="w-full" />
            </UFormField>
            <UFormField label="Parent">
              <USelect
                v-model="form.parent_id"
                :items="parentItems"
                :disabled="!canSave"
                class="w-full"
              />
            </UFormField>
            <UFormField label="Latitude">
              <UInput v-model="form.latitude" :disabled="!canSave" class="w-full" />
            </UFormField>
            <UFormField label="Longitude">
              <UInput v-model="form.longitude" :disabled="!canSave" class="w-full" />
            </UFormField>
            <div class="flex flex-wrap gap-2">
              <UButton
                label="Floor plans"
                size="sm"
                variant="outline"
                color="neutral"
                @click="router.push({ path: '/dcim/floor-plans', query: { site_id: selected.id } })"
              />
              <UButton
                label="Racks"
                size="sm"
                variant="outline"
                color="neutral"
                @click="router.push('/dcim/racks')"
              />
              <UButton
                label="Connections"
                size="sm"
                variant="outline"
                color="neutral"
                @click="router.push({ path: '/dcim/connections', query: { site_id: selected.id } })"
              />
            </div>
            <div v-if="canSave" class="flex justify-end">
              <UButton label="Save" :loading="saving" @click="saveSelected" />
            </div>
          </template>
        </div>
      </div>
    </div>
  </div>

  <div
    v-if="menu.open && menuItems.length"
    class="ipam-context-menu"
    :style="{ left: menu.x + 'px', top: menu.y + 'px' }"
    @click.stop
  >
    <template v-for="(item, i) in menuItems" :key="i">
      <hr v-if="item.id.startsWith('sep')" />
      <button v-else :class="{ danger: item.danger }" type="button" @click="runMenu(item.id)">
        {{ item.label }}
      </button>
    </template>
  </div>

  <FormModal
    :open="dialog === 'create'"
    :source="form"
    title="New site"
    @update:open="
      (v) => {
        if (!v) {
          dialog = null
          fillForm(selected)
        }
      }
    "
  >
    <template #body>
      <div class="flex flex-col gap-4">
        <UFormField label="Name">
          <UInput v-model="form.name" class="w-full" autofocus />
        </UFormField>
        <UFormField label="Parent">
          <USelect v-model="form.parent_id" :items="parentItems" class="w-full" />
        </UFormField>
        <UFormField label="Latitude">
          <UInput v-model="form.latitude" class="w-full" />
        </UFormField>
        <UFormField label="Longitude">
          <UInput v-model="form.longitude" class="w-full" />
        </UFormField>
      </div>
    </template>
    <template #footer>
      <UButton label="Cancel" variant="ghost" @click="dialog = null" />
      <UButton label="Add" :loading="saving" @click="saveCreate" />
    </template>
  </FormModal>

  <UModal :open="!!confirm" title="Delete" @update:open="(v) => !v && (confirm = null)">
    <template #body>
      Delete <strong>{{ confirm?.label }}</strong
      >?
    </template>
    <template #footer>
      <UButton label="Cancel" variant="ghost" @click="confirm = null" />
      <UButton label="Delete" color="error" :loading="saving" @click="performDelete" />
    </template>
  </UModal>
</template>
