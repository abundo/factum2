<script setup>
import { computed, onUnmounted, reactive, ref, watch } from 'vue'
import { getJobs, getJobTaskEvents } from '@/api/jobs'
import { formatDateTime, formatTime } from '@/utils/datetime'

const props = defineProps({
  job: { type: Object, default: null },
})

const open = defineModel('open', { type: Boolean, default: false })

// Local copy so polling can update events/counts/new sequential subjobs
// without waiting for the parent to hand in a fresh snapshot.
const liveJob = ref(null)

const activeTaskId = ref(null)
// Events are fetched on demand, one subjob at a time, and cached here for
// the life of the modal so switching tabs back and forth doesn't refetch -
// a job with several subjobs shouldn't pay for events nobody looks at.
const eventsByTaskId = reactive({})
const eventsLoadingByTaskId = reactive({})

// Same cadence as SyncOverviewPage: a running job's events and subjob
// badges should tick over without closing and reopening the modal.
const POLL_INTERVAL_MS = 3000
let pollTimer = null
let pollInFlight = false

const tabItems = computed(() =>
  (liveJob.value?.tasks ?? [])
    .map((task) => ({
      label: task.target,
      value: String(task.id),
      slot: String(task.id),
      task,
    }))
    .sort((a, b) => a.label.localeCompare(b.label)),
)

const eventColumns = [
  { id: 'level', header: 'Level' },
  { accessorKey: 'message', header: 'Message' },
  { id: 'time', header: 'Time' },
]

function eventColor(level) {
  switch (level) {
    case 'error':
      return 'error'
    case 'warning':
      return 'warning'
    default:
      return 'info'
  }
}

function clearEventCache() {
  for (const key of Object.keys(eventsByTaskId)) {
    delete eventsByTaskId[key]
  }
  for (const key of Object.keys(eventsLoadingByTaskId)) {
    delete eventsLoadingByTaskId[key]
  }
}

function loadTaskEvents(jobId, taskId, { force = false } = {}) {
  if (!force && eventsByTaskId[taskId]) {
    return Promise.resolve()
  }
  // Background refreshes keep the last table up; only the first fetch for
  // a tab should flip the loading spinner.
  const showLoading = !eventsByTaskId[taskId]
  if (showLoading) {
    eventsLoadingByTaskId[taskId] = true
  }
  return getJobTaskEvents(jobId, taskId)
    .then((data) => {
      eventsByTaskId[taskId] = data ?? []
    })
    .catch(() => {
      if (!eventsByTaskId[taskId]) {
        eventsByTaskId[taskId] = []
      }
    })
    .finally(() => {
      if (showLoading) {
        eventsLoadingByTaskId[taskId] = false
      }
    })
}

function onTabChange(value) {
  activeTaskId.value = value
  if (liveJob.value) {
    loadTaskEvents(liveJob.value.id, value, {
      force: !liveJob.value.finished_at,
    })
  }
}

function refreshRunningJob() {
  const job = liveJob.value
  if (pollInFlight || !open.value || !job || job.finished_at) {
    return
  }
  pollInFlight = true
  const taskIds = new Set(Object.keys(eventsByTaskId))
  if (activeTaskId.value) {
    taskIds.add(activeTaskId.value)
  }
  const eventsPromise = Promise.all(
    [...taskIds].map((taskId) => loadTaskEvents(job.id, taskId, { force: true })),
  )
  const jobPromise = getJobs()
    .then((data) => {
      const updated = (data ?? []).find((row) => row.id === job.id)
      if (updated) {
        liveJob.value = updated
      }
    })
    .catch(() => {
      // Keep the last good snapshot; this is a background poll.
    })
  Promise.all([eventsPromise, jobPromise]).finally(() => {
    pollInFlight = false
    if (!open.value || liveJob.value?.finished_at) {
      stopPolling()
    }
  })
}

function stopPolling() {
  if (pollTimer) {
    clearInterval(pollTimer)
    pollTimer = null
  }
}

function startPolling() {
  if (pollTimer) {
    return
  }
  pollTimer = setInterval(refreshRunningJob, POLL_INTERVAL_MS)
}

function syncPolling() {
  if (open.value && liveJob.value && !liveJob.value.finished_at) {
    startPolling()
  } else {
    stopPolling()
  }
}

// Reset the event cache when a different job is shown - same-id updates
// (parent refresh or our own poll) keep the cache so tabs don't flicker.
watch(
  () => props.job,
  (job, prev) => {
    if (job?.id !== prev?.id) {
      clearEventCache()
      const firstTask = job?.tasks?.[0]
      activeTaskId.value = firstTask ? String(firstTask.id) : null
      if (firstTask) {
        loadTaskEvents(job.id, String(firstTask.id))
      }
    }
    liveJob.value = job
    syncPolling()
  },
  { immediate: true },
)

watch(open, () => {
  syncPolling()
})

onUnmounted(stopPolling)
</script>

<template>
  <UModal
    v-model:open="open"
    :title="liveJob ? `Job #${liveJob.id} - ${formatDateTime(liveJob.started_at)}` : ''"
    :ui="{ content: 'w-[80vw] max-w-[80vw] h-[80vh] max-h-[80vh]' }"
  >
    <template #body>
      <UTabs
        v-if="liveJob"
        :items="tabItems"
        :model-value="activeTaskId"
        @update:model-value="onTabChange"
      >
        <template v-for="item in tabItems" :key="item.value" #[item.slot]>
          <div class="py-2 min-h-[24rem]">
            <UAlert
              v-if="item.task.err"
              color="error"
              variant="subtle"
              :title="item.task.err"
              class="mb-4"
            />
            <UTable
              :data="eventsByTaskId[item.value] ?? []"
              :columns="eventColumns"
              :loading="!!eventsLoadingByTaskId[item.value]"
              empty="No events reported for this subjob."
            >
              <template #level-cell="{ row }">
                <UBadge
                  :label="row.original.level"
                  :color="eventColor(row.original.level)"
                  variant="subtle"
                />
              </template>
              <template #time-cell="{ row }">{{ formatTime(row.original.at) }}</template>
            </UTable>
          </div>
        </template>

        <template #trailing="{ item }">
          <UBadge
            v-if="item.task.error_count"
            :label="item.task.error_count"
            color="error"
            variant="subtle"
            size="sm"
          />
          <UBadge
            v-if="item.task.warning_count"
            :label="item.task.warning_count"
            color="warning"
            variant="subtle"
            size="sm"
          />
        </template>
      </UTabs>
    </template>
    <template #footer>
      <UButton label="Close" icon="i-lucide-x" variant="ghost" @click="open = false" />
    </template>
  </UModal>
</template>
