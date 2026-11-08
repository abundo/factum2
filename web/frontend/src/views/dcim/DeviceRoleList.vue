<script setup>
import { computed, onMounted, ref } from 'vue'
import { useToast } from '@nuxt/ui/composables'
import {
  createDeviceRole,
  deleteDeviceRole,
  getDeviceRoles,
  updateDeviceRole,
} from '@/api/dcim'
import SearchInput from '@/components/SearchInput.vue'
import SortableColumnHeader from '@/components/SortableColumnHeader.vue'
import { useConfirm } from '@/composables/useConfirm'
import { useAuthStore } from '@/stores/auth'
import { useSearch, valuesText } from '@/utils/search'

defineOptions({ name: 'DeviceRoleList' })

const toast = useToast()
const authStore = useAuthStore()
const { confirmDelete } = useConfirm()

const items = ref([])
const loading = ref(true)
const error = ref(null)
const sorting = ref([{ id: 'name', desc: false }])

const columns = [
  { id: 'actions', header: '' },
  { accessorKey: 'name', header: 'Name' },
  { accessorKey: 'slug', header: 'Slug' },
  { accessorKey: 'color', header: 'Color' },
  { accessorKey: 'vm_role', header: 'VM' },
  { accessorKey: 'source', header: 'Source' },
]

const dialog = ref(false)
const form = ref(emptyForm())
const editingId = ref(null)
const saving = ref(false)
const deleting = ref(false)

const canWrite = computed(() => authStore.canWrite)
const dialogTitle = computed(() => (editingId.value ? 'Edit device role' : 'New device role'))
const editingNetbox = computed(() => {
  if (!editingId.value) return false
  return items.value.find((i) => i.id === editingId.value)?.source === 'netbox'
})
const { search, filtered } = useSearch(items, (row) =>
  valuesText(row.name, row.slug, row.description, row.source),
)

function emptyForm() {
  return { name: '', slug: '', color: '', description: '', vm_role: false, netbox: false }
}

function sourceBadgeColor(source) {
  if (source === 'factum') return 'success'
  return 'neutral'
}

function load() {
  loading.value = true
  error.value = null
  getDeviceRoles()
    .then((data) => {
      items.value = data ?? []
    })
    .catch(() => {
      error.value = 'Failed to load device roles.'
    })
    .finally(() => {
      loading.value = false
    })
}

function openNew() {
  editingId.value = null
  form.value = emptyForm()
  dialog.value = true
}

function openEdit(row) {
  editingId.value = row.id
  form.value = {
    name: row.name ?? '',
    slug: row.slug ?? '',
    color: row.color ?? '',
    description: row.description ?? '',
    vm_role: !!row.vm_role,
    netbox: row.source === 'netbox',
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
    color: form.value.color.trim(),
    description: form.value.description.trim(),
    vm_role: !!form.value.vm_role,
    netbox: !!form.value.netbox,
  }
  const req = editingId.value
    ? updateDeviceRole(editingId.value, payload)
    : createDeviceRole(payload)
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

async function remove() {
  const row = items.value.find((i) => i.id === editingId.value)
  if (!row) return
  if (!(await confirmDelete(`device role ${row.name}`))) return
  deleting.value = true
  deleteDeviceRole(row.id)
    .then(() => {
      dialog.value = false
      load()
    })
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
        <h4 class="m-0">Device roles</h4>
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
      :empty="error || (search && items.length ? 'Nothing matches the search.' : 'No device roles found.')"
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
      <template #color-cell="{ row }">
        <span v-if="row.original.color" class="inline-flex items-center gap-2">
          <span
            class="inline-block size-3 rounded-sm border border-default"
            :style="{ backgroundColor: `#${row.original.color}` }"
          />
          <span class="font-mono">{{ row.original.color }}</span>
        </span>
        <span v-else>—</span>
      </template>
      <template #vm_role-cell="{ row }">
        {{ row.original.vm_role ? 'Yes' : '—' }}
      </template>
      <template #source-cell="{ row }">
        <UBadge
          :label="row.original.source || '—'"
          :color="sourceBadgeColor(row.original.source)"
          variant="subtle"
        />
      </template>
      <template #actions-cell="{ row }">
        <UButton
          icon="i-lucide-pencil"
          variant="outline"
          size="sm"
          @click="openEdit(row.original)"
        />
      </template>
    </UTable>
  </div>

  <FormModal v-model:open="dialog" :source="form" :title="dialogTitle">
    <template #body>
      <div class="flex flex-col gap-4">
        <UFormField label="Name">
          <UInput v-model="form.name" class="w-full" autofocus />
        </UFormField>
        <UFormField label="Slug" hint="Leave blank to generate from the name">
          <UInput v-model="form.slug" class="w-full font-mono" />
        </UFormField>
        <UFormField label="Color" hint="6 hex digits, without #. Blank uses NetBox's default when the role is in NetBox.">
          <UInput v-model="form.color" class="w-full font-mono" maxlength="7" />
        </UFormField>
        <UFormField label="Description">
          <UTextarea v-model="form.description" :rows="3" class="w-full" />
        </UFormField>
        <UCheckbox v-model="form.vm_role" label="Can be assigned to virtual machines" />
        <UFormField hint="Create and keep this role in NetBox. Leave off for a Factum-only role.">
          <UCheckbox v-model="form.netbox" :disabled="editingNetbox" label="NetBox" />
        </UFormField>
      </div>
    </template>
    <template #footer="{ close }">
      <div class="flex w-full gap-2">
        <UButton
          v-if="editingId && canWrite"
          label="Delete"
          icon="i-lucide-trash"
          color="error"
          variant="ghost"
          :loading="deleting"
          @click="remove"
        />
        <UButton class="ms-auto" label="Cancel" icon="i-lucide-x" variant="ghost" @click="close" />
        <UButton
          v-if="canWrite"
          label="Save"
          icon="i-lucide-check"
          :loading="saving"
          @click="save"
        />
      </div>
    </template>
  </FormModal>
</template>
