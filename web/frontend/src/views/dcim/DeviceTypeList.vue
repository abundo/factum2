<script setup>
import { computed, onMounted, ref } from 'vue'
import { useToast } from '@nuxt/ui/composables'
import {
  createDeviceType,
  createDeviceTypeInterface,
  deleteDeviceType,
  deleteDeviceTypeInterface,
  getDeviceTypeInterfaces,
  getDeviceTypes,
  getManufacturers,
  updateDeviceType,
  updateDeviceTypeInterface,
} from '@/api/dcim'
import { interfaceTypeItems } from '@/utils/interfaceTypes'
import SearchInput from '@/components/SearchInput.vue'
import SortableColumnHeader from '@/components/SortableColumnHeader.vue'
import { useAuthStore } from '@/stores/auth'

defineOptions({ name: 'DeviceTypeList' })

const toast = useToast()
const authStore = useAuthStore()

const items = ref([])
const manufacturers = ref([])
const loading = ref(true)
const error = ref(null)
const globalFilter = ref('')
const sorting = ref([{ id: 'model', desc: false }])

const columns = [
  { id: 'actions', header: '' },
  { id: 'manufacturer', header: 'Manufacturer' },
  { accessorKey: 'model', header: 'Model' },
  { accessorKey: 'slug', header: 'Slug' },
  { accessorKey: 'source', header: 'Source' },
]

const dialog = ref(false)
const form = ref({ manufacturer_id: undefined, model: '', slug: '' })
const editingId = ref(null)
const saving = ref(false)
const deleting = ref(false)

const templatesDialog = ref(false)
const templatesDeviceType = ref(null)
const templates = ref([])
const templatesLoading = ref(false)
const templateFormOpen = ref(false)
const templateForm = ref({ name: '', type: '1000base-t', label: '', description: '' })
const templateEditingId = ref(null)
const templateSaving = ref(false)
const templateDeleting = ref(false)

const templateDialogTitle = computed(() =>
  templateEditingId.value ? 'Edit interface template' : 'New interface template',
)
const templateEditingLocal = computed(() => {
  if (!templateEditingId.value) return true
  return templates.value.find((i) => i.id === templateEditingId.value)?.source !== 'netbox'
})

const canWrite = computed(() => authStore.canWrite)
const dialogTitle = computed(() => (editingId.value ? 'Edit device type' : 'New device type'))
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
const manufacturerItems = computed(() =>
  manufacturers.value.map((m) => ({ label: m.name, value: m.id })),
)
const manufacturerById = computed(() => {
  const map = new Map()
  for (const m of manufacturers.value) map.set(m.id, m.name)
  return map
})

function load() {
  loading.value = true
  error.value = null
  Promise.all([getDeviceTypes(), getManufacturers()])
    .then(([types, mfrs]) => {
      items.value = types ?? []
      manufacturers.value = mfrs ?? []
    })
    .catch(() => {
      error.value = 'Failed to load device types.'
    })
    .finally(() => {
      loading.value = false
    })
}

function openNew() {
  editingId.value = null
  form.value = { manufacturer_id: undefined, model: '', slug: '' }
  dialog.value = true
}

function openEdit(row) {
  editingId.value = row.id
  form.value = {
    manufacturer_id: row.manufacturer_id,
    model: row.model ?? '',
    slug: row.slug ?? '',
  }
  dialog.value = true
}

function save() {
  if (!form.value.manufacturer_id) {
    toast.add({ color: 'error', title: 'Manufacturer is required' })
    return
  }
  if (!form.value.model.trim()) {
    toast.add({ color: 'error', title: 'Model is required' })
    return
  }
  saving.value = true
  const payload = {
    manufacturer_id: form.value.manufacturer_id,
    model: form.value.model.trim(),
    slug: form.value.slug.trim(),
  }
  const req = editingId.value
    ? updateDeviceType(editingId.value, payload)
    : createDeviceType(payload)
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
  deleteDeviceType(row.id)
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

function openTemplates(row) {
  templatesDeviceType.value = row
  templatesDialog.value = true
  loadTemplates()
}

function loadTemplates() {
  if (!templatesDeviceType.value) return
  templatesLoading.value = true
  getDeviceTypeInterfaces(templatesDeviceType.value.id)
    .then((data) => {
      templates.value = data ?? []
    })
    .catch(() => {
      toast.add({ color: 'error', title: 'Failed to load interface templates' })
    })
    .finally(() => {
      templatesLoading.value = false
    })
}

function openNewTemplate() {
  templateEditingId.value = null
  templateForm.value = { name: '', type: '1000base-t', label: '', description: '' }
  templateFormOpen.value = true
}

function openEditTemplate(row) {
  templateEditingId.value = row.id
  templateForm.value = {
    name: row.name ?? '',
    type: row.type || 'other',
    label: row.label ?? '',
    description: row.description ?? '',
  }
  templateFormOpen.value = true
}

function saveTemplate() {
  if (!templateForm.value.name.trim()) {
    toast.add({ color: 'error', title: 'Name is required' })
    return
  }
  templateSaving.value = true
  const payload = {
    name: templateForm.value.name.trim(),
    type: templateForm.value.type,
    label: templateForm.value.label.trim(),
    description: templateForm.value.description.trim(),
  }
  const req = templateEditingId.value
    ? updateDeviceTypeInterface(templateEditingId.value, payload)
    : createDeviceTypeInterface(templatesDeviceType.value.id, payload)
  req
    .then(() => {
      templateFormOpen.value = false
      loadTemplates()
    })
    .catch((err) => {
      toast.add({
        color: 'error',
        title: 'Save failed',
        description: err?.response?.data?.error,
      })
    })
    .finally(() => {
      templateSaving.value = false
    })
}

function removeTemplate(row) {
  templateDeleting.value = true
  deleteDeviceTypeInterface(row.id)
    .then(() => loadTemplates())
    .catch((err) => {
      toast.add({
        color: 'error',
        title: 'Delete failed',
        description: err?.response?.data?.error,
      })
    })
    .finally(() => {
      templateDeleting.value = false
    })
}

const templateColumns = [
  { id: 'actions', header: '' },
  { accessorKey: 'name', header: 'Name' },
  { accessorKey: 'type', header: 'Type' },
  { accessorKey: 'label', header: 'Label' },
  { accessorKey: 'description', header: 'Description' },
  { accessorKey: 'source', header: 'Source' },
]
const templateSorting = ref([{ id: 'name', desc: false }])

onMounted(load)
</script>

<template>
  <div class="card flex min-h-0 flex-1 flex-col overflow-hidden">
    <div class="flex flex-wrap gap-2 items-center justify-between mb-4 shrink-0">
      <div class="flex items-center gap-2">
        <h4 class="m-0">Device types</h4>
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
      :empty="error ?? 'No device types found.'"
      :virtualize="{ estimateSize: 46 }"
      sticky
      class="min-h-0 flex-1"
    >
      <template #model-header="{ column }">
        <SortableColumnHeader :column="column" label="Model" />
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
        {{ manufacturerById.get(row.original.manufacturer_id) || row.original.manufacturer_id }}
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
            label="Interfaces"
            size="sm"
            color="neutral"
            variant="outline"
            @click="openTemplates(row.original)"
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
        <UFormField label="Manufacturer">
          <USelect v-model="form.manufacturer_id" :items="manufacturerItems" class="w-full" />
        </UFormField>
        <UFormField label="Model">
          <UInput v-model="form.model" class="w-full" />
        </UFormField>
        <UFormField label="Slug" hint="Leave blank to generate from the model">
          <UInput v-model="form.slug" class="w-full font-mono" />
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

  <FormModal
    v-model:open="templatesDialog"
    :title="
      templatesDeviceType
        ? `${manufacturerById.get(templatesDeviceType.manufacturer_id) || ''} ${templatesDeviceType.model} — Interfaces`.trim()
        : 'Interface templates'
    "
    :ui="{ content: 'w-[90vw] sm:max-w-4xl' }"
  >
    <template #body>
      <div class="flex flex-col gap-3">
        <div class="flex justify-end">
          <UButton
            v-if="canWrite"
            label="New"
            icon="i-lucide-plus"
            color="neutral"
            size="sm"
            @click="openNewTemplate"
          />
        </div>
        <UTable
          v-model:sorting="templateSorting"
          :data="templates"
          :columns="templateColumns"
          :loading="templatesLoading"
          :empty="'No interface templates on this device type.'"
          sticky
        >
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
                @click="openEditTemplate(row.original)"
              />
              <UButton
                v-if="canWrite && isLocal(row.original)"
                icon="i-lucide-trash"
                variant="ghost"
                color="error"
                size="sm"
                :loading="templateDeleting"
                @click="removeTemplate(row.original)"
              />
            </div>
          </template>
        </UTable>
      </div>
    </template>
    <template #footer>
      <UButton label="Close" icon="i-lucide-x" variant="ghost" @click="templatesDialog = false" />
    </template>
  </FormModal>

  <FormModal
    v-model:open="templateFormOpen"
    :source="templateForm"
    :title="templateDialogTitle"
    :ui="{ content: 'sm:max-w-sm' }"
  >
    <template #body>
      <div class="flex flex-col gap-4">
        <UFormField label="Name">
          <UInput v-model="templateForm.name" class="w-full font-mono" autofocus />
        </UFormField>
        <UFormField label="Type">
          <USelect v-model="templateForm.type" :items="interfaceTypeItems" class="w-full" />
        </UFormField>
        <UFormField label="Label">
          <UInput v-model="templateForm.label" class="w-full" />
        </UFormField>
        <UFormField label="Description">
          <UInput v-model="templateForm.description" class="w-full" />
        </UFormField>
      </div>
    </template>
    <template #footer>
      <UButton label="Cancel" icon="i-lucide-x" variant="ghost" @click="templateFormOpen = false" />
      <UButton
        v-if="canWrite && templateEditingLocal"
        label="Save"
        icon="i-lucide-check"
        :loading="templateSaving"
        @click="saveTemplate"
      />
    </template>
  </FormModal>
</template>
