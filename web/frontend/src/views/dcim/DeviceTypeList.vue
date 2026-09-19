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
  getPlatforms,
  updateDeviceType,
  updateDeviceTypeInterface,
} from '@/api/dcim'
import DcimDetailDialog from '@/components/DcimDetailDialog.vue'
import InterfaceEditorDialog from '@/components/InterfaceEditorDialog.vue'
import SearchInput from '@/components/SearchInput.vue'
import SortableColumnHeader from '@/components/SortableColumnHeader.vue'
import { useAuthStore } from '@/stores/auth'

defineOptions({ name: 'DeviceTypeList' })

const toast = useToast()
const authStore = useAuthStore()

const items = ref([])
const manufacturers = ref([])
const platforms = ref([])
const loading = ref(true)
const error = ref(null)
const globalFilter = ref('')
const sorting = ref([{ id: 'model', desc: false }])

const columns = [
  { id: 'actions', header: '' },
  { id: 'manufacturer', header: 'Manufacturer' },
  { accessorKey: 'model', header: 'Model' },
  { id: 'platform', header: 'Platform' },
  { accessorKey: 'slug', header: 'Slug' },
  { accessorKey: 'source', header: 'Source' },
]

const createDialog = ref(false)
const form = ref({ manufacturer_id: undefined, model: '', slug: '', platform_id: 0 })
const editingId = ref(null)
const saving = ref(false)
const deleting = ref(false)

const detailDialog = ref(false)
const detailTab = ref('overview')
const detailType = ref(null)

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
const editingLocal = computed(() => {
  if (!editingId.value) return true
  return items.value.find((i) => i.id === editingId.value)?.source !== 'netbox'
})

const detailTabItems = [
  { label: 'Overview', value: 'overview', slot: 'overview' },
  { label: 'Interfaces', value: 'interfaces', slot: 'interfaces' },
]

const detailTitle = computed(() => {
  if (!detailType.value) return 'Device type'
  const mfr = manufacturerById.value.get(detailType.value.manufacturer_id) || ''
  return `${mfr} ${detailType.value.model}`.trim()
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
const platformItems = computed(() => [
  { label: 'None', value: 0 },
  ...platforms.value.map((p) => ({ label: `${p.name} (${p.slug})`, value: p.id })),
])
const platformById = computed(() => {
  const map = new Map()
  for (const p of platforms.value) map.set(p.id, p)
  return map
})

function emptyTypeForm() {
  return { manufacturer_id: undefined, model: '', slug: '', platform_id: 0 }
}

function fillFormFromType(row) {
  form.value = {
    manufacturer_id: row.manufacturer_id,
    model: row.model ?? '',
    slug: row.slug ?? '',
    platform_id: row.platform_id || 0,
  }
}

function load() {
  loading.value = true
  error.value = null
  Promise.all([getDeviceTypes(), getManufacturers(), getPlatforms()])
    .then(([types, mfrs, plats]) => {
      items.value = types ?? []
      manufacturers.value = mfrs ?? []
      platforms.value = plats ?? []
      if (editingId.value) {
        const row = items.value.find((i) => i.id === editingId.value)
        if (row) {
          detailType.value = row
          fillFormFromType(row)
        }
      }
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
  detailType.value = null
  form.value = emptyTypeForm()
  createDialog.value = true
}

function openDetail(row) {
  editingId.value = row.id
  detailType.value = row
  fillFormFromType(row)
  detailTab.value = 'overview'
  detailDialog.value = true
  loadTemplates()
}

function typePayload() {
  return {
    manufacturer_id: form.value.manufacturer_id,
    model: form.value.model.trim(),
    slug: form.value.slug.trim(),
    platform_id: form.value.platform_id || 0,
  }
}

function validateTypeForm() {
  if (!form.value.manufacturer_id) {
    toast.add({ color: 'error', title: 'Manufacturer is required' })
    return false
  }
  if (!form.value.model.trim()) {
    toast.add({ color: 'error', title: 'Model is required' })
    return false
  }
  return true
}

function saveNew() {
  if (!validateTypeForm()) return
  saving.value = true
  createDeviceType(typePayload())
    .then(() => {
      createDialog.value = false
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

function saveDetail() {
  if (!editingId.value) return
  if (!validateTypeForm()) return
  saving.value = true
  updateDeviceType(editingId.value, typePayload())
    .then(() => {
      load()
      toast.add({ color: 'success', title: 'Device type saved', duration: 3000 })
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

function removeDetail() {
  if (!editingId.value) return
  deleting.value = true
  deleteDeviceType(editingId.value)
    .then(() => {
      detailDialog.value = false
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

function loadTemplates() {
  if (!detailType.value) return
  templatesLoading.value = true
  getDeviceTypeInterfaces(detailType.value.id)
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
    : createDeviceTypeInterface(detailType.value.id, payload)
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
      <template #platform-cell="{ row }">
        {{
          platformById.get(row.original.platform_id)
            ? `${platformById.get(row.original.platform_id).name} (${platformById.get(row.original.platform_id).slug})`
            : '—'
        }}
      </template>
      <template #actions-cell="{ row }">
        <UButton
          icon="i-lucide-pencil"
          variant="outline"
          color="neutral"
          size="sm"
          @click="openDetail(row.original)"
        />
      </template>
    </UTable>
  </div>

  <FormModal
    v-model:open="createDialog"
    :source="form"
    title="New device type"
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
        <UFormField label="Platform">
          <USelect v-model="form.platform_id" :items="platformItems" class="w-full" />
        </UFormField>
        <UFormField label="Slug" hint="Leave blank to generate from the model">
          <UInput v-model="form.slug" class="w-full font-mono" />
        </UFormField>
      </div>
    </template>
    <template #footer>
      <UButton label="Cancel" icon="i-lucide-x" variant="ghost" @click="createDialog = false" />
      <UButton
        v-if="canWrite"
        label="Create"
        icon="i-lucide-check"
        :loading="saving"
        @click="saveNew"
      />
    </template>
  </FormModal>

  <DcimDetailDialog
    v-model:open="detailDialog"
    v-model:tab="detailTab"
    :tabs="detailTabItems"
    :ready="!!detailType"
    :title="detailTitle"
  >
    <template #title>
      <div class="flex min-w-0 flex-wrap items-baseline gap-x-4 gap-y-0.5 pr-2">
        <span class="truncate">{{ detailTitle }}</span>
        <UBadge
          v-if="detailType?.source"
          :label="detailType.source"
          :color="sourceBadgeColor(detailType.source)"
          variant="subtle"
        />
      </div>
    </template>
    <template #overview>
      <div class="grid grid-cols-[9rem_minmax(0,1fr)] items-center gap-y-3 gap-x-3">
        <label class="font-bold whitespace-nowrap">Manufacturer</label>
        <USelect
          v-if="canWrite && editingLocal"
          v-model="form.manufacturer_id"
          :items="manufacturerItems"
          class="w-full"
        />
        <UInput
          v-else
          :model-value="manufacturerById.get(form.manufacturer_id) || ''"
          disabled
          class="w-full"
        />

        <label class="font-bold whitespace-nowrap">Model</label>
        <UInput v-model="form.model" class="w-full" :disabled="!(canWrite && editingLocal)" />

        <label class="font-bold whitespace-nowrap">Platform</label>
        <USelect
          v-if="canWrite && editingLocal"
          v-model="form.platform_id"
          :items="platformItems"
          class="w-full"
        />
        <UInput
          v-else
          :model-value="
            platformById.get(form.platform_id)
              ? `${platformById.get(form.platform_id).name} (${platformById.get(form.platform_id).slug})`
              : '—'
          "
          disabled
          class="w-full"
        />

        <label class="font-bold whitespace-nowrap">Slug</label>
        <UInput
          v-model="form.slug"
          class="w-full font-mono"
          :disabled="!(canWrite && editingLocal)"
        />
      </div>
    </template>
    <template #interfaces>
      <div class="flex flex-col h-full min-h-0">
        <div class="flex flex-wrap items-end gap-2 mb-4 shrink-0">
          <UButton
            v-if="canWrite"
            label="New"
            icon="i-lucide-plus"
            size="sm"
            color="neutral"
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
          class="flex-1 min-h-0 overflow-y-auto"
        >
          <template #source-cell="{ row }">
            <UBadge
              :label="row.original.source || '—'"
              :color="sourceBadgeColor(row.original.source)"
              variant="subtle"
            />
          </template>
          <template #actions-cell="{ row }">
            <div class="flex gap-1">
              <UButton
                icon="i-lucide-pencil"
                variant="ghost"
                color="neutral"
                size="sm"
                title="Edit interface"
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
      <UButton
        v-if="canWrite && editingLocal"
        label="Delete"
        icon="i-lucide-trash"
        color="error"
        variant="ghost"
        :loading="deleting"
        @click="removeDetail"
      />
      <UButton
        v-if="canWrite && editingLocal"
        label="Save"
        icon="i-lucide-check"
        :loading="saving"
        @click="saveDetail"
      />
      <UButton label="Close" icon="i-lucide-x" variant="ghost" @click="detailDialog = false" />
    </template>
  </DcimDetailDialog>

  <InterfaceEditorDialog
    v-model:open="templateFormOpen"
    v-model:form="templateForm"
    :title="templateDialogTitle"
    kind="template"
    :writable="templateEditingLocal"
    :saving="templateSaving"
    :editing="!!templateEditingId"
    :can-write="canWrite"
    @save="saveTemplate"
  />
</template>
