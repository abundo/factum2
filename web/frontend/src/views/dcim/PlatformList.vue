<script setup>
import { computed, onMounted, ref } from 'vue'
import { useToast } from '@nuxt/ui/composables'
import {
  createPlatform,
  deletePlatform,
  getManufacturers,
  getPlatforms,
  updatePlatform,
} from '@/api/dcim'
import SearchInput from '@/components/SearchInput.vue'
import SortableColumnHeader from '@/components/SortableColumnHeader.vue'
import { useAuthStore } from '@/stores/auth'

defineOptions({ name: 'PlatformList' })

const toast = useToast()
const authStore = useAuthStore()

const items = ref([])
const manufacturers = ref([])
const loading = ref(true)
const error = ref(null)
const globalFilter = ref('')
const sorting = ref([{ id: 'name', desc: false }])

const columns = [
  { id: 'actions', header: '' },
  { accessorKey: 'name', header: 'Name' },
  { accessorKey: 'slug', header: 'Slug' },
  { id: 'manufacturer', header: 'Manufacturer' },
  { accessorKey: 'source', header: 'Source' },
]

const dialog = ref(false)
const form = ref({ name: '', slug: '', manufacturer_id: 0 })
const editingId = ref(null)
const saving = ref(false)
const deleting = ref(false)

const canWrite = computed(() => authStore.canWrite)
const dialogTitle = computed(() => (editingId.value ? 'Edit platform' : 'New platform'))
const editingLocal = computed(() => {
  if (!editingId.value) return true
  return items.value.find((i) => i.id === editingId.value)?.source !== 'netbox'
})

function isLocal(row) {
  return row.source !== 'netbox'
}

function sourceBadgeColor(source) {
  if (source === 'factum') return 'success'
  return 'neutral'
}
const manufacturerItems = computed(() => [
  { label: 'None', value: 0 },
  ...manufacturers.value.map((m) => ({ label: m.name, value: m.id })),
])
const manufacturerById = computed(() => {
  const map = new Map()
  for (const m of manufacturers.value) map.set(m.id, m.name)
  return map
})

function load() {
  loading.value = true
  error.value = null
  Promise.all([getPlatforms(), getManufacturers()])
    .then(([plats, mfrs]) => {
      items.value = plats ?? []
      manufacturers.value = mfrs ?? []
    })
    .catch(() => {
      error.value = 'Failed to load platforms.'
    })
    .finally(() => {
      loading.value = false
    })
}

function openNew() {
  editingId.value = null
  form.value = { name: '', slug: '', manufacturer_id: 0 }
  dialog.value = true
}

function openEdit(row) {
  editingId.value = row.id
  form.value = {
    name: row.name ?? '',
    slug: row.slug ?? '',
    manufacturer_id: row.manufacturer_id ?? 0,
  }
  dialog.value = true
}

function save() {
  if (!form.value.name.trim()) {
    toast.add({ color: 'error', title: 'Name is required' })
    return
  }
  saving.value = true
  const payload = {
    name: form.value.name.trim(),
    slug: form.value.slug.trim(),
    manufacturer_id: form.value.manufacturer_id || 0,
  }
  const req = editingId.value ? updatePlatform(editingId.value, payload) : createPlatform(payload)
  req
    .then(() => {
      dialog.value = false
      load()
    })
    .catch((err) => {
      toast.add({
        color: 'error',
        title: 'Save failed',
        description: err?.response?.data?.error,
      })
    })
    .finally(() => {
      saving.value = false
    })
}

function remove(row) {
  deleting.value = true
  deletePlatform(row.id)
    .then(() => load())
    .catch((err) => {
      toast.add({
        color: 'error',
        title: 'Delete failed',
        description: err?.response?.data?.error,
      })
    })
    .finally(() => {
      deleting.value = false
    })
}

onMounted(load)
</script>

<template>
  <div class="card flex min-h-0 flex-1 flex-col overflow-hidden">
    <div class="flex flex-wrap gap-2 items-center justify-between mb-4 shrink-0">
      <div class="flex items-center gap-2">
        <h4 class="m-0">Platforms</h4>
        <UButton
          v-if="canWrite"
          label="New"
          icon="i-lucide-plus"
          color="neutral"
          size="sm"
          @click="openNew"
        />
      </div>
      <SearchInput v-model="globalFilter" />
    </div>

    <UTable
      v-model:sorting="sorting"
      v-model:global-filter="globalFilter"
      :data="items"
      :columns="columns"
      :loading="loading"
      :empty="error ?? 'No platforms found.'"
      :virtualize="{ estimateSize: 46 }"
      sticky
      class="min-h-0 flex-1"
    >
      <template #name-header="{ column }">
        <SortableColumnHeader :column="column" label="Name" />
      </template>
      <template #slug-header="{ column }">
        <SortableColumnHeader :column="column" label="Slug" />
      </template>
      <template #source-header="{ column }">
        <SortableColumnHeader :column="column" label="Source" />
      </template>
      <template #source-cell="{ row }">
        <UBadge
          :label="row.original.source || '—'"
          :color="sourceBadgeColor(row.original.source)"
          variant="subtle"
        />
      </template>
      <template #manufacturer-cell="{ row }">
        {{ manufacturerById.get(row.original.manufacturer_id) || '—' }}
      </template>
      <template #actions-cell="{ row }">
        <div class="flex gap-2">
          <UButton
            icon="i-lucide-pencil"
            variant="outline"
            color="neutral"
            size="sm"
            @click="openEdit(row.original)"
          />
          <UButton
            v-if="canWrite && isLocal(row.original)"
            icon="i-lucide-trash"
            variant="ghost"
            color="error"
            size="sm"
            :loading="deleting"
            @click="remove(row.original)"
          />
        </div>
      </template>
    </UTable>
  </div>

  <FormModal v-model:open="dialog" :source="form" :title="dialogTitle" :ui="{ content: 'sm:max-w-sm' }">
    <template #body>
      <div class="flex flex-col gap-4">
        <UFormField label="Name">
          <UInput v-model="form.name" class="w-full" autofocus />
        </UFormField>
        <UFormField label="Slug" hint="Driver match key (eos, sros, vrp, …). Blank generates from name.">
          <UInput v-model="form.slug" class="w-full font-mono" />
        </UFormField>
        <UFormField label="Manufacturer">
          <USelect v-model="form.manufacturer_id" :items="manufacturerItems" class="w-full" />
        </UFormField>
      </div>
    </template>
    <template #footer>
      <UButton label="Cancel" icon="i-lucide-x" variant="ghost" @click="dialog = false" />
      <UButton
        v-if="canWrite && editingLocal"
        label="Save"
        icon="i-lucide-check"
        :loading="saving"
        @click="save"
      />
    </template>
  </FormModal>
</template>
