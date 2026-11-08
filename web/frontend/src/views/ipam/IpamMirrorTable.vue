<script setup>
import { onMounted, ref } from 'vue'
import SearchInput from '@/components/SearchInput.vue'
import SortableColumnHeader from '@/components/SortableColumnHeader.vue'
import { useSearch } from '@/utils/search'

const props = defineProps({
  title: { type: String, required: true },
  load: { type: Function, required: true },
  columns: { type: Array, required: true },
  searchText: { type: Function, required: true },
  empty: { type: String, required: true },
  failed: { type: String, required: true },
})

const items = ref([])
const loading = ref(true)
const error = ref(null)
const firstKey = props.columns.find((col) => col.accessorKey)?.accessorKey || 'name'
const sorting = ref([{ id: firstKey, desc: false }])
const { search, filtered } = useSearch(items, (row) => props.searchText(row))

function sourceBadgeColor(source) {
  if (source === 'factum') return 'success'
  return 'neutral'
}

function headerLabel(key, fallback) {
  return props.columns.find((col) => col.accessorKey === key)?.header || fallback
}

onMounted(() => {
  props
    .load()
    .then((rows) => {
      items.value = rows ?? []
    })
    .catch(() => {
      error.value = props.failed
    })
    .finally(() => {
      loading.value = false
    })
})
</script>

<template>
  <div class="card flex min-h-0 flex-1 flex-col overflow-hidden">
    <div class="mb-4 flex shrink-0 flex-wrap items-center justify-between gap-2">
      <h4 class="m-0">{{ title }}</h4>
      <SearchInput v-model="search" />
    </div>

    <UTable
      v-model:sorting="sorting"
      :data="filtered"
      :columns="columns"
      :loading="loading"
      :empty="error || (search && items.length ? 'Nothing matches the search.' : empty)"
      :virtualize="{ estimateSize: 46 }"
      sticky
      class="min-h-0 flex-1"
    >
      <template #name-header="{ column }">
        <SortableColumnHeader :column="column" :label="headerLabel('name', 'Name')" />
      </template>
      <template #slug-header="{ column }">
        <SortableColumnHeader :column="column" :label="headerLabel('slug', 'Slug')" />
      </template>
      <template #rir-header="{ column }">
        <SortableColumnHeader :column="column" :label="headerLabel('rir', 'RIR')" />
      </template>
      <template #start-header="{ column }">
        <SortableColumnHeader :column="column" :label="headerLabel('start', 'Start')" />
      </template>
      <template #end-header="{ column }">
        <SortableColumnHeader :column="column" :label="headerLabel('end', 'End')" />
      </template>
      <template #tenant-header="{ column }">
        <SortableColumnHeader :column="column" :label="headerLabel('tenant', 'Tenant')" />
      </template>
      <template #description-header="{ column }">
        <SortableColumnHeader :column="column" :label="headerLabel('description', 'Description')" />
      </template>
      <template #asn-header="{ column }">
        <SortableColumnHeader :column="column" :label="headerLabel('asn', 'ASN')" />
      </template>
      <template #role-header="{ column }">
        <SortableColumnHeader :column="column" :label="headerLabel('role', 'Role')" />
      </template>
      <template #sites-header="{ column }">
        <SortableColumnHeader :column="column" :label="headerLabel('sites', 'Sites')" />
      </template>
      <template #source-header="{ column }">
        <SortableColumnHeader :column="column" :label="headerLabel('source', 'Source')" />
      </template>
      <template #source-cell="{ row }">
        <UBadge
          :label="row.original.source || '—'"
          :color="sourceBadgeColor(row.original.source)"
          variant="subtle"
        />
      </template>
    </UTable>
  </div>
</template>
