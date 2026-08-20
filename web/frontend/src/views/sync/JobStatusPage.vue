<script setup>
import { onMounted, ref } from 'vue'
import { getJobs, getWorkerStatus } from '@/api/jobs'
import SortableColumnHeader from '@/components/SortableColumnHeader.vue'
import JobDetailModal from './JobDetailModal.vue'
import { formatDateTime } from '@/utils/datetime'

const nodes = ref([])
const loading = ref(true)
const loadError = ref(false)
const nodeSorting = ref([{ id: 'name', desc: false }])

const nodeColumns = [
  { accessorKey: 'name', header: 'Name' },
  { accessorKey: 'address', header: 'Address' },
  { id: 'enabled', header: 'Enabled' },
  { id: 'connected', header: 'Connected' },
  { accessorKey: 'hostname', header: 'Hostname' },
  { id: 'roles', header: 'Roles' },
  { id: 'last_seen', header: 'Last seen' },
]

function loadStatus() {
  loading.value = true
  loadError.value = false
  getWorkerStatus()
    .then((data) => {
      nodes.value = data ?? []
    })
    .catch(() => {
      loadError.value = true
    })
    .finally(() => {
      loading.value = false
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

function loadJobs() {
  jobsLoading.value = true
  jobsLoadError.value = false
  getJobs()
    .then((data) => {
      jobs.value = data ?? []
    })
    .catch(() => {
      jobsLoadError.value = true
    })
    .finally(() => {
      jobsLoading.value = false
    })
}

function jobTasks(job) {
  return job.tasks ?? []
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

function jobStatus(job) {
  if (!job.finished_at) {
    return { label: 'Running', color: 'info' }
  }
  const failed = jobTasks(job).some((task) => task.exit_code !== 0)
  if (failed) {
    return { label: 'Failed', color: 'error' }
  }
  return { label: 'Success', color: 'success' }
}

function jobDuration(job) {
  if (!job.finished_at) {
    return '-'
  }
  const seconds = (new Date(job.finished_at) - new Date(job.started_at)) / 1000
  if (seconds < 1) {
    return '<1s'
  }
  return `${Math.round(seconds)}s`
}

const detailDialog = ref(false)
const selectedJob = ref(null)

function viewJob(job) {
  selectedJob.value = job
  detailDialog.value = true
}

onMounted(() => {
  loadStatus()
  loadJobs()
})
</script>

<template>
  <div class="card mb-6">
    <div class="flex items-center justify-between mb-4">
      <div class="font-semibold text-xl">Job status</div>
      <UButton label="Refresh" icon="i-lucide-refresh-cw" :loading="loading" @click="loadStatus" />
    </div>

    <UAlert v-if="loadError" color="error" variant="subtle" title="Failed to load worker node status." class="mb-4" />

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
    </UTable>
  </div>

  <div class="card">
    <div class="flex items-center justify-between mb-4">
      <div class="font-semibold text-xl">Recent jobs</div>
      <UButton label="Refresh" icon="i-lucide-refresh-cw" :loading="jobsLoading" @click="loadJobs" />
    </div>

    <UAlert v-if="jobsLoadError" color="error" variant="subtle" title="Failed to load job history." class="mb-4" />

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
        <UBadge :label="jobStatus(row.original).label" :color="jobStatus(row.original).color" variant="subtle" />
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
