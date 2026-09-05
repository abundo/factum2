<script setup>
import { onMounted, onUnmounted, ref } from 'vue'
import { getJobs, getWorkerStatus } from '@/api/jobs'
import SortableColumnHeader from '@/components/SortableColumnHeader.vue'
import JobDetailModal from './JobDetailModal.vue'
import { formatDateTime } from '@/utils/datetime'
import { jobDuration, jobStatus, jobTasks } from '@/utils/job'

const REFRESH_INTERVAL_MS = 5000
const AUTO_REFRESH_DURATION_MS = 60 * 60 * 1000

const nodes = ref([])
const loading = ref(true)
const loadError = ref(false)
const nodeSorting = ref([{ id: 'name', desc: false }])
const autoRefresh = ref(false)

let refreshTimer = null
let stopTimer = null
let refreshInFlight = false

const nodeColumns = [
  { accessorKey: 'name', header: 'Name' },
  { accessorKey: 'address', header: 'Address' },
  { id: 'enabled', header: 'Enabled' },
  { id: 'connected', header: 'Connected' },
  { accessorKey: 'hostname', header: 'Hostname' },
  { id: 'roles', header: 'Roles' },
  { id: 'last_seen', header: 'Last seen' },
  { accessorKey: 'last_error', header: 'Last error' },
]

function loadStatus({ showLoading = false } = {}) {
  if (showLoading) {
    loading.value = true
    loadError.value = false
  }
  return getWorkerStatus()
    .then((data) => {
      nodes.value = data ?? []
    })
    .catch(() => {
      if (showLoading) {
        loadError.value = true
      }
    })
    .finally(() => {
      if (showLoading) {
        loading.value = false
      }
    })
}

function formatLastSeen(value) {
  if (!value || value.startsWith('0001-01-01')) {
    return 'Never'
  }
  return formatDateTime(value)
}

const jobs = ref([])
const jobsLoading = ref(true)
const jobsLoadError = ref(false)
const jobSorting = ref([{ id: 'started', desc: true }])

const jobColumns = [
  { id: 'actions', header: '' },
  { id: 'targets', header: 'Targets' },
  { id: 'triggered_by', header: 'Triggered by' },
  { id: 'started', accessorKey: 'started_at', header: 'Started' },
  { id: 'status', header: 'Status' },
  { id: 'duration', header: 'Duration' },
  { id: 'errors', header: 'Errors' },
  { id: 'warnings', header: 'Warnings' },
]

function loadJobs({ showLoading = false } = {}) {
  if (showLoading) {
    jobsLoading.value = true
    jobsLoadError.value = false
  }
  return getJobs()
    .then((data) => {
      jobs.value = data ?? []
    })
    .catch(() => {
      if (showLoading) {
        jobsLoadError.value = true
      }
    })
    .finally(() => {
      if (showLoading) {
        jobsLoading.value = false
      }
    })
}

function loadAll({ showLoading = false } = {}) {
  if (refreshInFlight) {
    return Promise.resolve()
  }
  refreshInFlight = true
  return Promise.all([loadStatus({ showLoading }), loadJobs({ showLoading })]).finally(() => {
    refreshInFlight = false
  })
}

function clearRefreshTimers() {
  if (refreshTimer) {
    clearInterval(refreshTimer)
    refreshTimer = null
  }
  if (stopTimer) {
    clearTimeout(stopTimer)
    stopTimer = null
  }
}

function stopAutoRefresh() {
  autoRefresh.value = false
  clearRefreshTimers()
}

function startAutoRefresh({ refreshNow = false } = {}) {
  clearRefreshTimers()
  autoRefresh.value = true
  if (refreshNow) {
    loadAll()
  }
  refreshTimer = setInterval(() => {
    loadAll()
  }, REFRESH_INTERVAL_MS)
  stopTimer = setTimeout(stopAutoRefresh, AUTO_REFRESH_DURATION_MS)
}

function setAutoRefresh(on) {
  if (on) {
    startAutoRefresh({ refreshNow: true })
  } else {
    stopAutoRefresh()
  }
}

function jobTargets(job) {
  return jobTasks(job)
    .map((task) => task.target)
    .join(', ')
}

function jobErrorCount(job) {
  return jobTasks(job).reduce((sum, task) => sum + (task.error_count || 0), 0)
}

function jobWarningCount(job) {
  return jobTasks(job).reduce((sum, task) => sum + (task.warning_count || 0), 0)
}

const detailDialog = ref(false)
const selectedJob = ref(null)

function viewJob(job) {
  selectedJob.value = job
  detailDialog.value = true
}

onMounted(() => {
  loadAll({ showLoading: true })
  startAutoRefresh()
})

onUnmounted(() => {
  stopAutoRefresh()
})
</script>

<template>
  <div class="card mb-6">
    <div class="flex items-center justify-between gap-3 mb-4 flex-wrap">
      <div class="font-semibold text-xl">Job status</div>
      <div
        class="flex items-center gap-2 shrink-0 whitespace-nowrap"
        title="Refresh every 5 seconds (stops after 1 hour)"
      >
        <USwitch
          id="auto-refresh"
          :model-value="autoRefresh"
          @update:model-value="setAutoRefresh"
        />
        <label for="auto-refresh" class="cursor-pointer select-none">Auto-refresh</label>
        <span v-if="autoRefresh" class="text-sm text-muted-color">every 5s</span>
      </div>
    </div>

    <UAlert
      v-if="loadError"
      color="error"
      variant="subtle"
      title="Failed to load worker node status."
      class="mb-4"
    />

    <UTable
      v-model:sorting="nodeSorting"
      :data="nodes"
      :columns="nodeColumns"
      :loading="loading"
      empty="No worker nodes configured."
    >
      <template #name-header="{ column }">
        <SortableColumnHeader :column="column" label="Name" />
      </template>
      <template #address-header="{ column }">
        <SortableColumnHeader :column="column" label="Address" />
      </template>

      <template #enabled-cell="{ row }">
        <UBadge
          :label="row.original.enabled ? 'Yes' : 'No'"
          :color="row.original.enabled ? 'success' : 'neutral'"
          variant="subtle"
        />
      </template>
      <template #connected-cell="{ row }">
        <UBadge
          :label="row.original.connected ? 'Connected' : 'Disconnected'"
          :color="row.original.connected ? 'success' : 'error'"
          variant="subtle"
        />
      </template>
      <template #roles-cell="{ row }">
        <div class="flex flex-wrap gap-1">
          <UBadge v-for="role in row.original.roles" :key="role" :label="role" variant="subtle" />
        </div>
      </template>
      <template #last_seen-cell="{ row }">{{ formatLastSeen(row.original.last_seen) }}</template>
      <template #last_error-cell="{ row }">
        <span v-if="row.original.last_error" class="text-red-500 text-sm whitespace-pre-wrap">{{
          row.original.last_error
        }}</span>
      </template>
    </UTable>
  </div>

  <div class="card">
    <div class="font-semibold text-xl mb-4">Recent jobs</div>

    <UAlert
      v-if="jobsLoadError"
      color="error"
      variant="subtle"
      title="Failed to load job history."
      class="mb-4"
    />

    <UTable
      v-model:sorting="jobSorting"
      :data="jobs"
      :columns="jobColumns"
      :loading="jobsLoading"
      empty="No jobs yet."
    >
      <template #started-header="{ column }">
        <SortableColumnHeader :column="column" label="Started" />
      </template>

      <template #actions-cell="{ row }">
        <UButton
          icon="i-lucide-list"
          variant="outline"
          color="neutral"
          size="sm"
          @click="viewJob(row.original)"
        />
      </template>
      <template #targets-cell="{ row }">{{ jobTargets(row.original) || '-' }}</template>
      <template #triggered_by-cell="{ row }">{{ row.original.triggered_by || '-' }}</template>
      <template #started-cell="{ row }">{{ formatDateTime(row.original.started_at) }}</template>
      <template #status-cell="{ row }">
        <UBadge
          :label="jobStatus(row.original).label"
          :color="jobStatus(row.original).color"
          variant="subtle"
        />
      </template>
      <template #duration-cell="{ row }">{{ jobDuration(row.original) }}</template>
      <template #errors-cell="{ row }">
        <UBadge
          v-if="jobErrorCount(row.original)"
          :label="jobErrorCount(row.original)"
          color="error"
          variant="subtle"
        />
        <span v-else>-</span>
      </template>
      <template #warnings-cell="{ row }">
        <UBadge
          v-if="jobWarningCount(row.original)"
          :label="jobWarningCount(row.original)"
          color="warning"
          variant="subtle"
        />
        <span v-else>-</span>
      </template>
    </UTable>
  </div>

  <JobDetailModal v-model:open="detailDialog" :job="selectedJob" />
</template>
