<script setup>
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { useToast } from '@nuxt/ui/composables'
import { createRack, getRacks } from '@/api/racks'
import { getSites } from '@/api/sites'
import FormModal from '@/components/FormModal.vue'
import SearchInput from '@/components/SearchInput.vue'
import SortableColumnHeader from '@/components/SortableColumnHeader.vue'
import { useAuthStore } from '@/stores/auth'
import { useSearch, valuesText } from '@/utils/search'

defineOptions({ name: 'RackListPage' })

const toast = useToast()
const router = useRouter()
const authStore = useAuthStore()
const canWrite = computed(() => authStore.canWrite)

const items = ref([])
const sites = ref([])
const loading = ref(true)
const error = ref(null)
const sorting = ref([{ id: 'name', desc: false }])
const { search, filtered } = useSearch(items, (row) =>
  valuesText(
    row.name,
    row.site_name,
    row.height_u != null ? `${row.height_u}U` : '',
    row.occupancy_percent != null ? `${row.occupancy_percent}%` : '',
    row.unknown_dimensions ? 'unknown height' : '',
    row.source,
  ),
)
const dialog = ref(false)
const saving = ref(false)
const form = ref(emptyForm())

const columns = [
  { id: 'actions', header: '' },
  { accessorKey: 'name', header: 'Name' },
  { accessorKey: 'site_name', header: 'Site' },
  { accessorKey: 'height_u', header: 'Height' },
  { accessorKey: 'occupancy_percent', header: 'Occupancy' },
  { accessorKey: 'source', header: 'Source' },
]

const siteItems = computed(() => sites.value.map((s) => ({ label: s.name, value: s.id })))

function emptyForm() {
  return { name: '', site_id: undefined, height_u: 42, numbering: 'ascending' }
}

function errMsg(err, fallback) {
  return err.response?.data?.error ?? fallback
}

function sourceBadgeColor(source) {
  return source === 'factum' ? 'success' : 'neutral'
}

function load() {
  loading.value = true
  Promise.all([getRacks(), getSites()])
    .then(([racks, siteRows]) => {
      items.value = racks ?? []
      sites.value = siteRows ?? []
      error.value = null
    })
    .catch(() => {
      error.value = 'Failed to load racks.'
    })
    .finally(() => {
      loading.value = false
    })
}

function openNew() {
  form.value = emptyForm()
  dialog.value = true
}

function save() {
  if (!form.value.name?.trim() || !form.value.site_id) {
    toast.add({ color: 'error', title: 'Name and site are required' })
    return
  }
  saving.value = true
  createRack({
    name: form.value.name.trim(),
    site_id: form.value.site_id,
    height_u: Number(form.value.height_u) || 42,
    numbering: form.value.numbering,
  })
    .then((row) => {
      dialog.value = false
      router.push(`/dcim/racks/${row.id}`)
    })
    .catch((err) => toast.add({ color: 'error', title: 'Create failed', description: errMsg(err) }))
    .finally(() => {
      saving.value = false
    })
}

onMounted(load)
</script>

<template>
  <div class="card flex min-h-0 flex-1 flex-col overflow-hidden">
    <div class="flex flex-wrap gap-2 items-center justify-between mb-4 shrink-0">
      <div class="flex items-center gap-2">
        <h4 class="m-0">Racks</h4>
        <UButton
          v-if="canWrite"
          label="New"
          icon="i-lucide-plus"
          color="neutral"
          size="sm"
          @click="openNew"
        />
      </div>
      <SearchInput v-model="search" />
    </div>
    <UTable
      v-model:sorting="sorting"
      :data="filtered"
      :columns="columns"
      :loading="loading"
      :empty="error || (search && items.length ? 'Nothing matches the search.' : 'No racks found.')"
      :virtualize="{ estimateSize: 46 }"
      sticky
      class="min-h-0 flex-1"
    >
      <template #name-header="{ column }">
        <SortableColumnHeader :column="column" label="Name" />
      </template>
      <template #occupancy_percent-cell="{ row }">
        {{ row.original.occupancy_percent }}%
        <span v-if="row.original.unknown_dimensions" class="text-warning text-xs ml-1"
          >unknown height</span
        >
      </template>
      <template #height_u-cell="{ row }"> {{ row.original.height_u }}U </template>
      <template #source-cell="{ row }">
        <UBadge
          :label="row.original.source || '—'"
          :color="sourceBadgeColor(row.original.source)"
          variant="subtle"
        />
      </template>
      <template #actions-cell="{ row }">
        <UButton
          icon="i-lucide-rows-3"
          variant="outline"
          color="neutral"
          size="sm"
          aria-label="Open rack"
          @click="router.push(`/dcim/racks/${row.original.id}`)"
        />
      </template>
    </UTable>
  </div>

  <FormModal v-model:open="dialog" :source="form" title="New rack">
    <template #body>
      <div class="flex flex-col gap-4">
        <UFormField label="Name">
          <UInput v-model="form.name" class="w-full" autofocus />
        </UFormField>
        <UFormField label="Site">
          <USelect v-model="form.site_id" :items="siteItems" class="w-full" />
        </UFormField>
        <UFormField label="Height (U)">
          <UInput v-model="form.height_u" type="number" class="w-full" />
        </UFormField>
        <UFormField label="Numbering">
          <USelect
            v-model="form.numbering"
            :items="[
              { label: 'Ascending (U1 at bottom)', value: 'ascending' },
              { label: 'Descending (U1 at top)', value: 'descending' },
            ]"
            class="w-full"
          />
        </UFormField>
      </div>
    </template>
    <template #footer="{ close }">
      <div class="flex w-full gap-2">
        <UButton class="ms-auto" label="Cancel" variant="ghost" @click="close" />
        <UButton label="Create" :loading="saving" @click="save" />
      </div>
    </template>
  </FormModal>
</template>
