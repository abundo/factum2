<script setup>
import { onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { getOxidizedNodes } from '@/api/oxidized'
import OxidizedNodeDialog from '@/components/OxidizedNodeDialog.vue'
import SortableColumnHeader from '@/components/SortableColumnHeader.vue'
import {
  apiError,
  formatOxidizedTime,
  nodeKey,
  nodeMatches,
  statusColor,
} from '@/utils/oxidized'

defineOptions({ name: 'OxidizedBrowserPage' })

const route = useRoute()
const router = useRouter()

const nodes = ref([])
const loading = ref(true)
const error = ref(null)
const globalFilter = ref('')
const sorting = ref([{ id: 'name', desc: false }])

const columns = [
  { id: 'actions', header: '' },
  { accessorKey: 'name', header: 'Name' },
  { accessorKey: 'ip', header: 'IP' },
  { accessorKey: 'model', header: 'Model' },
  { accessorKey: 'group', header: 'Group' },
  { accessorKey: 'status', header: 'Status' },
  { accessorKey: 'time', header: 'Last backup' },
  { accessorKey: 'mtime', header: 'Last change' },
]

const detailOpen = ref(false)
const selected = ref(null)

function load() {
  loading.value = true
  error.value = null
  getOxidizedNodes()
    .then((data) => {
      nodes.value = data ?? []
    })
    .catch((err) => {
      error.value = apiError(err, 'Failed to load Oxidized nodes.')
      nodes.value = []
    })
    .finally(() => {
      loading.value = false
    })
}

function openNode(node) {
  selected.value = node
  detailOpen.value = true
  const key = nodeKey(node)
  if (route.query.node !== key) {
    router.replace({ query: { ...route.query, node: key } })
  }
}

watch(detailOpen, (open) => {
  if (!open && route.query.node) {
    const q = { ...route.query }
    delete q.node
    router.replace({ query: q })
  }
})

onMounted(() => {
  load()
})

watch(
  () => [loading.value, nodes.value, route.query.node],
  () => {
    const want = route.query.node
    if (loading.value || !want || detailOpen.value) {
      return
    }
    const match = nodes.value.find((n) => nodeMatches(n, want))
    if (match) {
      openNode(match)
    }
  },
)
</script>

<template>
  <div class="card">
    <div class="flex flex-wrap gap-2 items-center justify-between mb-4">
      <h4 class="m-0">Oxidized</h4>
      <div class="flex gap-2 items-center">
        <UButton
          icon="i-lucide-refresh-cw"
          variant="outline"
          color="neutral"
          size="sm"
          :loading="loading"
          @click="load"
        />
        <UInput v-model="globalFilter" icon="i-lucide-search" placeholder="Search..." />
      </div>
    </div>

    <p class="text-muted-color mb-4">
      Device configurations stored by Oxidized, including version history and diffs. Oxidized's API
      only lists devices currently in its source (router.db); configs of removed devices stay in git
      unless
      <span class="font-mono">clean_obsolete_nodes</span>
      is enabled, but they cannot be browsed here.
    </p>

    <UAlert v-if="error" color="error" variant="subtle" :title="error" class="mb-4" />

    <UTable
      v-model:sorting="sorting"
      v-model:global-filter="globalFilter"
      :data="nodes"
      :columns="columns"
      :loading="loading"
      :empty="error ?? 'No devices in Oxidized.'"
      :virtualize="{ estimateSize: 46 }"
      class="max-h-[calc(100vh-320px)]"
    >
      <template
        v-for="col in columns.filter((c) => c.id !== 'actions')"
        :key="col.accessorKey"
        #[`${col.accessorKey}-header`]="{ column }"
      >
        <SortableColumnHeader :column="column" :label="col.header" />
      </template>

      <template #actions-cell="{ row }">
        <UButton
          label="Browse"
          icon="i-lucide-file-search"
          size="sm"
          variant="outline"
          color="neutral"
          @click="openNode(row.original)"
        />
      </template>
      <template #status-cell="{ row }">
        <UBadge
          v-if="row.original.status"
          :label="row.original.status"
          :color="statusColor(row.original.status)"
          variant="subtle"
        />
      </template>
      <template #time-cell="{ row }">
        {{ formatOxidizedTime(row.original.time) }}
      </template>
      <template #mtime-cell="{ row }">
        {{ formatOxidizedTime(row.original.mtime) }}
      </template>
    </UTable>
  </div>

  <OxidizedNodeDialog v-model:open="detailOpen" :node="selected" />
</template>
