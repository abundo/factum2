<script setup>
import { useToast } from '@nuxt/ui/composables'
import { onMounted, ref } from 'vue'
import { createKindMap, deleteKindMap, listKindMaps, updateKindMap } from '@/api/optical'
import SearchInput from '@/components/SearchInput.vue'
import { useConfirm } from '@/composables/useConfirm'
import { useSearch, valuesText } from '@/utils/search'

const toast = useToast()
const { confirmDelete } = useConfirm()
const rows = ref([])
const loading = ref(true)
const roleName = ref('')
const kind = ref('wdm_shelf')
const dialog = ref(false)
const editing = ref(null)
const formKind = ref('wdm_shelf')
const saving = ref(false)
const deleting = ref(false)

const kindOptions = [
  { label: 'WDM shelf (TXP/MXP chassis)', value: 'wdm_shelf' },
  { label: 'ROADM', value: 'roadm' },
  { label: 'ILA / amplifier', value: 'ila' },
  { label: 'Passive / ODF', value: 'passive' },
]

function kindLabel(value) {
  return kindOptions.find((option) => option.value === value)?.label ?? value
}

const { search, filtered } = useSearch(rows, (row) =>
  valuesText(row.netbox_role_name, kindLabel(row.optical_kind), row.optical_kind),
)

function load() {
  loading.value = true
  listKindMaps()
    .then((data) => {
      rows.value = data ?? []
    })
    .catch((err) => {
      toast.add({
        color: 'error',
        title: 'Failed to load maps',
        description: err?.response?.data?.error,
      })
    })
    .finally(() => {
      loading.value = false
    })
}

function add() {
  createKindMap({ netbox_role_name: roleName.value, optical_kind: kind.value })
    .then(() => {
      roleName.value = ''
      load()
    })
    .catch((err) => {
      toast.add({ color: 'error', title: 'Save failed', description: err?.response?.data?.error })
    })
}

function openEdit(row) {
  editing.value = row
  formKind.value = row.optical_kind
  dialog.value = true
}

function saveEdit() {
  if (!editing.value) return
  saving.value = true
  updateKindMap(editing.value.id, { optical_kind: formKind.value })
    .then(() => {
      dialog.value = false
      load()
    })
    .catch((err) => {
      toast.add({ color: 'error', title: 'Save failed', description: err?.response?.data?.error })
    })
    .finally(() => {
      saving.value = false
    })
}

async function remove() {
  if (!editing.value) return
  const name = editing.value.netbox_role_name || 'map'
  if (!(await confirmDelete(`optical kind map ${name}`))) return
  deleting.value = true
  deleteKindMap(editing.value.id)
    .then(() => {
      dialog.value = false
      load()
    })
    .catch((err) => {
      toast.add({ color: 'error', title: 'Delete failed', description: err?.response?.data?.error })
    })
    .finally(() => {
      deleting.value = false
    })
}

onMounted(load)
</script>

<template>
  <div class="card">
    <div class="font-semibold text-lg mb-3">Optical kind maps</div>
    <p class="text-muted-color mb-4">
      Map a NetBox device role display name to a Factum chassis kind. Prefer the NetBox
      <code>optical_role</code> custom field (on device and interface) when you have it — this table
      is the fallback. Cables Factum walks must be interface↔interface (not Front/Rear ports).
    </p>
    <div class="flex flex-wrap gap-3 mb-6">
      <UInput v-model="roleName" placeholder="NetBox role name" class="w-64" />
      <USelect
        v-model="kind"
        :items="kindOptions"
        value-key="value"
        label-key="label"
        class="w-64"
      />
      <UButton label="Add" :disabled="!roleName" @click="add" />
    </div>
    <SearchInput v-model="search" class="mb-3 max-w-xs" />
    <UTable
      :data="filtered"
      :loading="loading"
      :empty="search && rows.length ? 'Nothing matches the search.' : 'No kind maps yet.'"
      :columns="[
        { id: 'actions', header: '' },
        { accessorKey: 'netbox_role_name', header: 'NetBox role' },
        { accessorKey: 'optical_kind', header: 'Kind' },
      ]"
    >
      <template #optical_kind-cell="{ row }">
        {{ kindLabel(row.original.optical_kind) }}
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

  <FormModal v-model:open="dialog" :source="formKind" title="Optical kind map">
    <template #body>
      <div class="flex flex-col gap-4">
        <UFormField label="NetBox role">
          <UInput :model-value="editing?.netbox_role_name" disabled class="w-full" />
        </UFormField>
        <UFormField label="Kind">
          <USelect
            v-model="formKind"
            :items="kindOptions"
            value-key="value"
            label-key="label"
            class="w-full"
          />
        </UFormField>
      </div>
    </template>
    <template #footer="{ close }">
      <div class="flex w-full gap-2">
        <UButton
          v-if="editing"
          label="Delete"
          icon="i-lucide-trash"
          color="error"
          variant="ghost"
          :loading="deleting"
          @click="remove"
        />
        <UButton class="ms-auto" label="Cancel" icon="i-lucide-x" variant="ghost" @click="close" />
        <UButton label="Save" icon="i-lucide-check" :loading="saving" @click="saveEdit" />
      </div>
    </template>
  </FormModal>
</template>
