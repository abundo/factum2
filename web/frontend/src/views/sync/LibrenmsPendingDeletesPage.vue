<script setup>
import { computed, onMounted, ref } from 'vue'
import { useToast } from '@nuxt/ui/composables'
import { deletePendingNextSync, getPendingDeletes } from '@/api/librenms'
import SearchInput from '@/components/SearchInput.vue'
import SortableColumnHeader from '@/components/SortableColumnHeader.vue'
import { useAuthStore } from '@/stores/auth'
import { formatDateTime } from '@/utils/datetime'

defineOptions({ name: 'LibrenmsPendingDeletesPage' })

const toast = useToast()
const authStore = useAuthStore()

const reasonLabels = {
  no_match: 'No matching factum device',
  disabled: 'Disabled in factum',
  not_monitored: 'Not monitored in LibreNMS',
}

const rows = ref([])
const loading = ref(true)
const error = ref(null)
const globalFilter = ref('')
const sorting = ref([{ id: 'scheduled_at', desc: false }])

const confirmOpen = ref(false)
const confirming = ref(false)
const selected = ref(null)

const canWrite = computed(() => authStore.canWrite)

const columns = [
  { id: 'actions', header: '' },
  { accessorKey: 'hostname', header: 'Hostname' },
  { accessorKey: 'display', header: 'Display name' },
  { accessorKey: 'reason', header: 'Reason' },
  { accessorKey: 'scheduled_at', header: 'Scheduled' },
  { id: 'status', header: 'Status' },
]

function reasonLabel(reason) {
  return reasonLabels[reason] ?? reason
}

function isDue(row) {
  if (!row?.scheduled_at) {
    return false
  }
  return new Date(row.scheduled_at).getTime() <= Date.now()
}

function statusLabel(row) {
  if (row.force_delete) {
    return 'Queued for next sync'
  }
  if (isDue(row)) {
    return 'Due'
  }
  return 'Waiting'
}

function statusColor(row) {
  if (row.force_delete || isDue(row)) {
    return 'error'
  }
  return 'warning'
}

function load() {
  loading.value = true
  error.value = null
  getPendingDeletes()
    .then((data) => {
      rows.value = data ?? []
    })
    .catch(() => {
      error.value = 'Failed to load pending deletions.'
    })
    .finally(() => {
      loading.value = false
    })
}

function openConfirm(row) {
  selected.value = row
  confirmOpen.value = true
}

function queueDelete() {
  if (!selected.value) {
    return
  }
  confirming.value = true
  deletePendingNextSync(selected.value.device_id)
    .then(() => {
      toast.add({
        color: 'success',
        title: 'Queued',
        description: 'Device will be deleted from LibreNMS on the next sync.',
        duration: 4000,
      })
      confirmOpen.value = false
      load()
    })
    .catch((err) => {
      toast.add({
        color: 'error',
        title: 'Error',
        description: err.response?.data?.error ?? 'Failed to queue deletion.',
        duration: 4000,
      })
    })
    .finally(() => {
      confirming.value = false
    })
}

onMounted(load)
</script>

<template>
  <div class="card">
    <div class="flex flex-wrap gap-2 items-center justify-between mb-4">
      <h4 class="m-0">Device deletions</h4>
      <SearchInput v-model="globalFilter" />
    </div>

    <p class="text-muted-color mb-4">
      Devices that would be removed from LibreNMS are disabled first, with
      <span class="font-mono">(scheduled for deletion YYYY-MM-DD)</span> on the display name. They
      are deleted automatically after the configured delay, or on the next sync if you queue them
      here. Graphs and collected data are lost when the device is actually deleted.
    </p>

    <UTable
      v-model:sorting="sorting"
      v-model:global-filter="globalFilter"
      :data="rows"
      :columns="columns"
      :loading="loading"
      :empty="error ?? 'No devices pending deletion.'"
      :virtualize="{ estimateSize: 46 }"
      class="max-h-[calc(100vh-380px)]"
    >
      <template #hostname-header="{ column }">
        <SortableColumnHeader :column="column" label="Hostname" />
      </template>
      <template #display-header="{ column }">
        <SortableColumnHeader :column="column" label="Display name" />
      </template>
      <template #reason-header="{ column }">
        <SortableColumnHeader :column="column" label="Reason" />
      </template>
      <template #reason-cell="{ row }">
        {{ reasonLabel(row.original.reason) }}
      </template>
      <template #scheduled_at-header="{ column }">
        <SortableColumnHeader :column="column" label="Scheduled" />
      </template>
      <template #scheduled_at-cell="{ row }">
        {{ row.original.scheduled_at ? formatDateTime(row.original.scheduled_at) : '—' }}
      </template>
      <template #status-cell="{ row }">
        <UBadge
          :label="statusLabel(row.original)"
          :color="statusColor(row.original)"
          variant="subtle"
        />
      </template>
      <template #actions-cell="{ row }">
        <UButton
          v-if="canWrite"
          label="Delete next sync"
          icon="i-lucide-trash-2"
          variant="outline"
          color="error"
          size="sm"
          :disabled="row.original.force_delete"
          @click="openConfirm(row.original)"
        />
      </template>
    </UTable>
  </div>

  <UModal v-model:open="confirmOpen" title="Delete on next sync" :ui="{ content: 'sm:max-w-md' }">
    <template #body>
      <p>
        Queue
        <span class="font-semibold">{{ selected?.display || selected?.hostname }}</span>
        for deletion on the next LibreNMS sync? Graphs and collected data in LibreNMS will be
        permanently lost.
      </p>
    </template>
    <template #footer>
      <div class="flex w-full justify-end gap-2">
        <UButton label="Cancel" icon="i-lucide-x" variant="ghost" @click="confirmOpen = false" />
        <UButton
          label="Queue deletion"
          icon="i-lucide-trash-2"
          color="error"
          :loading="confirming"
          @click="queueDelete"
        />
      </div>
    </template>
  </UModal>
</template>
