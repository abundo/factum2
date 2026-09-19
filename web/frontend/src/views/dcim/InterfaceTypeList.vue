<script setup>
import { computed, onMounted, ref } from 'vue'
import { useToast } from '@nuxt/ui/composables'
import {
  createInterfaceType,
  deleteInterfaceType,
  getInterfaceTypes,
  updateInterfaceType,
} from '@/api/dcim'
import SearchInput from '@/components/SearchInput.vue'
import SortableColumnHeader from '@/components/SortableColumnHeader.vue'
import { useAuthStore } from '@/stores/auth'

defineOptions({ name: 'InterfaceTypeList' })

const toast = useToast()
const authStore = useAuthStore()

const items = ref([])
const loading = ref(true)
const error = ref(null)
const globalFilter = ref('')
const sorting = ref([{ id: 'sort_order', desc: false }])

const columns = [
  { id: 'actions', header: '' },
  { accessorKey: 'label', header: 'Label' },
  { accessorKey: 'value', header: 'Value' },
  { accessorKey: 'source', header: 'Source' },
]

const dialog = ref(false)
const form = ref({ value: '', label: '' })
const editingId = ref(null)
const saving = ref(false)
const deleting = ref(false)

const canWrite = computed(() => authStore.canWrite)
const dialogTitle = computed(() => (editingId.value ? 'Edit interface type' : 'New interface type'))
const editingLocal = computed(() => {
  if (!editingId.value) return true
  return items.value.find((i) => i.id === editingId.value)?.source !== 'netbox'
})

function load() {
  loading.value = true
  error.value = null
  getInterfaceTypes()
    .then((data) => {
      items.value = data ?? []
    })
    .catch(() => {
      error.value = 'Failed to load interface types.'
    })
    .finally(() => {
      loading.value = false
    })
}

function openNew() {
  editingId.value = null
  form.value = { value: '', label: '' }
  dialog.value = true
}

function isLocal(row) {
  return row.source !== 'netbox'
}

function sourceBadgeColor(source) {
  if (source === 'factum') return 'success'
  if (source === 'netbox') return 'neutral'
  return 'neutral'
}

function openEdit(row) {
  editingId.value = row.id
  form.value = { value: row.value ?? '', label: row.label ?? '' }
  dialog.value = true
}

function save() {
  if (!form.value.value.trim()) {
    toast.add({ color: 'error', title: 'Value is required' })
    return
  }
  saving.value = true
  const payload = {
    value: form.value.value.trim(),
    label: form.value.label.trim() || form.value.value.trim(),
  }
  const req = editingId.value
    ? updateInterfaceType(editingId.value, payload)
    : createInterfaceType(payload)
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
  deleteInterfaceType(row.id)
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
        <h4 class="m-0">Interface types</h4>
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
      :empty="error ?? 'No interface types found.'"
      :virtualize="{ estimateSize: 46 }"
      sticky
      class="min-h-0 flex-1"
    >
      <template #label-header="{ column }">
        <SortableColumnHeader :column="column" label="Label" />
      </template>
      <template #value-header="{ column }">
        <SortableColumnHeader :column="column" label="Value" />
      </template>
      <template #source-header="{ column }">
        <SortableColumnHeader :column="column" label="Source" />
      </template>
      <template #value-cell="{ row }">
        <span class="font-mono">{{ row.original.value }}</span>
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
  </div>

  <FormModal
    v-model:open="dialog"
    :source="form"
    :title="dialogTitle"
    :ui="{ content: 'sm:max-w-sm' }"
  >
    <template #body>
      <div class="flex flex-col gap-4">
        <UFormField label="Label">
          <UInput v-model="form.label" class="w-full" autofocus :disabled="!editingLocal" />
        </UFormField>
        <UFormField
          label="Value"
          hint="Stored on interfaces. Use a NetBox slug such as 1000base-t when this will be synced."
        >
          <UInput v-model="form.value" class="w-full font-mono" :disabled="!editingLocal" />
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
