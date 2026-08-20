<script setup>
import { useToast } from '@nuxt/ui/composables'
import { onMounted, ref } from 'vue'
import { createKindMap, deleteKindMap, listKindMaps, updateKindMap } from '@/api/optical'

const toast = useToast()
const rows = ref([])
const loading = ref(true)
const roleName = ref('')
const kind = ref('wdm_shelf')

const kindOptions = [
  { label: 'WDM shelf (TXP/MXP chassis)', value: 'wdm_shelf' },
  { label: 'ROADM', value: 'roadm' },
  { label: 'ILA / amplifier', value: 'ila' },
  { label: 'Passive / ODF', value: 'passive' },
]

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

function changeKind(row, optical_kind) {
  updateKindMap(row.id, { optical_kind }).then(load)
}

function remove(row) {
  deleteKindMap(row.id).then(load)
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
    <UTable
      :data="rows"
      :loading="loading"
      :columns="[
        { accessorKey: 'netbox_role_name', header: 'NetBox role' },
        { accessorKey: 'optical_kind', header: 'Kind' },
        { id: 'actions', header: '' },
      ]"
    >
      <template #optical_kind-cell="{ row }">
        <USelect
          :model-value="row.original.optical_kind"
          :items="kindOptions"
          value-key="value"
          label-key="label"
          @update:model-value="changeKind(row.original, $event)"
        />
      </template>
      <template #actions-cell="{ row }">
        <UButton
          icon="i-lucide-trash"
          color="error"
          variant="ghost"
          size="sm"
          @click="remove(row.original)"
        />
      </template>
    </UTable>
  </div>
</template>
