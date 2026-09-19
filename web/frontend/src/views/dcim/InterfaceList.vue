<script setup>
import { computed, onMounted, ref, watch } from 'vue'
import { useToast } from '@nuxt/ui/composables'
import { getDevices } from '@/api/devices'
import { createInterface, deleteInterface, getInterfaces, updateInterface } from '@/api/dcim'
import SearchInput from '@/components/SearchInput.vue'
import SortableColumnHeader from '@/components/SortableColumnHeader.vue'
import { useAuthStore } from '@/stores/auth'
import { interfaceTypeItems } from '@/utils/interfaceTypes'

defineOptions({ name: 'InterfaceList' })

const toast = useToast()
const authStore = useAuthStore()

const items = ref([])
const devices = ref([])
const loading = ref(true)
const error = ref(null)
const globalFilter = ref('')
const sorting = ref([{ id: 'device_name', desc: false }])
const page = ref(1)
const pageSize = ref(100)
const total = ref(0)
let searchTimer = null

const pageSizeItems = [50, 100, 250]

const pageFrom = computed(() => (total.value === 0 ? 0 : (page.value - 1) * pageSize.value + 1))
const pageTo = computed(() => Math.min(page.value * pageSize.value, total.value))

const columns = [
  { id: 'actions', header: '' },
  { accessorKey: 'device_name', header: 'Device' },
  { accessorKey: 'name', header: 'Name' },
  { accessorKey: 'type', header: 'Type' },
  { accessorKey: 'description', header: 'Description' },
  { accessorKey: 'enabled', header: 'Enabled' },
  { accessorKey: 'source', header: 'Source' },
]

const dialog = ref(false)
const form = ref({
  device_id: undefined,
  name: '',
  type: '1000base-t',
  label: '',
  description: '',
  vrf: '',
  enabled: true,
})
const editingId = ref(null)
const saving = ref(false)
const deleting = ref(false)

const canWrite = computed(() => authStore.canWrite)
const dialogTitle = computed(() => (editingId.value ? 'Edit interface' : 'New interface'))
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

const localDeviceItems = computed(() =>
  devices.value
    .filter((d) => !d.netbox_id && d.cf_source !== 'netbox')
    .map((d) => ({ label: d.name, value: d.id })),
)

function loadDevices() {
  return getDevices().then((devs) => {
    devices.value = devs ?? []
  })
}

function load() {
  loading.value = true
  error.value = null
  const sort = sorting.value?.[0]
  getInterfaces({
    limit: pageSize.value,
    offset: (page.value - 1) * pageSize.value,
    q: globalFilter.value.trim() || undefined,
    sort: sort?.id || 'device_name',
    desc: sort?.desc ? true : undefined,
  })
    .then((data) => {
      items.value = data?.items ?? []
      total.value = data?.total ?? 0
    })
    .catch(() => {
      error.value = 'Failed to load interfaces.'
    })
    .finally(() => {
      loading.value = false
    })
}

function openNew() {
  editingId.value = null
  form.value = {
    device_id: undefined,
    name: '',
    type: '1000base-t',
    label: '',
    description: '',
    vrf: '',
    enabled: true,
  }
  dialog.value = true
}

function openEdit(row) {
  editingId.value = row.id
  form.value = {
    device_id: row.device_id,
    name: row.name ?? '',
    type: row.type || 'other',
    label: row.label ?? '',
    description: row.description ?? '',
    vrf: row.vrf ?? '',
    enabled: !!row.enabled,
  }
  dialog.value = true
}

function save() {
  if (!editingId.value && !form.value.device_id) {
    toast.add({ color: 'error', title: 'Device is required' })
    return
  }
  if (!form.value.name.trim()) {
    toast.add({ color: 'error', title: 'Name is required' })
    return
  }
  saving.value = true
  const payload = {
    device_id: form.value.device_id,
    name: form.value.name.trim(),
    type: form.value.type,
    label: form.value.label.trim(),
    description: form.value.description.trim(),
    vrf: form.value.vrf.trim(),
    enabled: form.value.enabled,
  }
  const req = editingId.value ? updateInterface(editingId.value, payload) : createInterface(payload)
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
  deleteInterface(row.id)
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

watch([page, pageSize, sorting], load, { deep: true })

watch(globalFilter, () => {
  clearTimeout(searchTimer)
  searchTimer = setTimeout(() => {
    if (page.value !== 1) {
      page.value = 1
      return
    }
    load()
  }, 300)
})

onMounted(() => {
  loadDevices()
  load()
})
</script>

<template>
  <div class="card flex min-h-0 flex-1 flex-col overflow-hidden">
    <div class="flex flex-wrap gap-2 items-center justify-between mb-4 shrink-0">
      <div class="flex items-center gap-2">
        <h4 class="m-0">Interfaces</h4>
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
      :data="items"
      :columns="columns"
      :loading="loading"
      :empty="error ?? 'No interfaces found.'"
      :virtualize="{ estimateSize: 46 }"
      sticky
      class="min-h-0 flex-1"
    >
      <template #device_name-header="{ column }">
        <SortableColumnHeader :column="column" label="Device" />
      </template>
      <template #name-header="{ column }">
        <SortableColumnHeader :column="column" label="Name" />
      </template>
      <template #type-header="{ column }">
        <SortableColumnHeader :column="column" label="Type" />
      </template>
      <template #source-header="{ column }">
        <SortableColumnHeader :column="column" label="Source" />
      </template>
      <template #enabled-cell="{ row }">
        <UBadge
          :label="row.original.enabled ? 'yes' : 'no'"
          :color="row.original.enabled ? 'success' : 'neutral'"
          variant="subtle"
        />
      </template>
      <template #source-cell="{ row }">
        <UBadge
          :label="row.original.source || '—'"
          :color="sourceBadgeColor(row.original.source)"
          variant="subtle"
        />
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

    <div class="flex flex-wrap items-center justify-between gap-3 mt-3 shrink-0">
      <div class="flex items-center gap-2 text-sm text-muted">
        <span>{{ pageFrom }}–{{ pageTo }} of {{ total }}</span>
        <USelect
          v-model="pageSize"
          :items="pageSizeItems"
          class="w-24"
          @update:model-value="page = 1"
        />
      </div>
      <UPagination
        v-model:page="page"
        :total="total"
        :items-per-page="pageSize"
        :disabled="loading"
      />
    </div>
  </div>

  <FormModal
    v-model:open="dialog"
    :source="form"
    :title="dialogTitle"
    :ui="{ content: 'sm:max-w-sm' }"
  >
    <template #body>
      <div class="flex flex-col gap-4">
        <UFormField v-if="!editingId" label="Device">
          <USelect v-model="form.device_id" :items="localDeviceItems" class="w-full" />
        </UFormField>
        <UFormField v-else label="Device">
          <UInput
            :model-value="items.find((i) => i.id === editingId)?.device_name || ''"
            disabled
            class="w-full"
          />
        </UFormField>
        <UFormField label="Name">
          <UInput v-model="form.name" class="w-full font-mono" autofocus />
        </UFormField>
        <UFormField label="Type">
          <USelect v-model="form.type" :items="interfaceTypeItems" class="w-full" />
        </UFormField>
        <UFormField label="Label">
          <UInput v-model="form.label" class="w-full" />
        </UFormField>
        <UFormField label="Description">
          <UInput v-model="form.description" class="w-full" />
        </UFormField>
        <UFormField label="VRF">
          <UInput v-model="form.vrf" class="w-full" :disabled="!!editingId && !editingLocal" />
        </UFormField>
        <UFormField label="Enabled">
          <USwitch v-model="form.enabled" />
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
