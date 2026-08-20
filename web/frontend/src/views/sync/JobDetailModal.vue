<script setup>
import { computed, reactive, ref, watch } from 'vue'
import { getJobTaskEvents } from '@/api/jobs'
import { formatDateTime, formatTime } from '@/utils/datetime'

const props = defineProps({
  job: { type: Object, default: null },
})

const open = defineModel('open', { type: Boolean, default: false })

const activeTaskId = ref(null)
// Events are fetched on demand, one subjob at a time, and cached here for
// the life of the modal so switching tabs back and forth doesn't refetch -
// a job with several subjobs shouldn't pay for events nobody looks at.
const eventsByTaskId = reactive({})
const eventsLoadingByTaskId = reactive({})

const tabItems = computed(() =>
  (props.job?.tasks ?? [])
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

function loadTaskEvents(jobId, taskId) {
  if (eventsByTaskId[taskId]) {
    return
  }
  eventsLoadingByTaskId[taskId] = true
  getJobTaskEvents(jobId, taskId)
    .then((data) => {
      eventsByTaskId[taskId] = data ?? []
    })
    .catch(() => {
      eventsByTaskId[taskId] = []
    })
    .finally(() => {
      eventsLoadingByTaskId[taskId] = false
    })
}

function onTabChange(value) {
  activeTaskId.value = value
  if (props.job) {
    loadTaskEvents(props.job.id, value)
  }
}

// A fresh job is shown each time the modal opens - reset the event cache
// and eagerly load the first (default) tab.
watch(
  () => props.job,
  (job) => {
    for (const key of Object.keys(eventsByTaskId)) {
      delete eventsByTaskId[key]
    }
    for (const key of Object.keys(eventsLoadingByTaskId)) {
      delete eventsLoadingByTaskId[key]
    }
    const firstTask = job?.tasks?.[0]
    activeTaskId.value = firstTask ? String(firstTask.id) : null
    if (firstTask) {
      loadTaskEvents(job.id, String(firstTask.id))
    }
  },
)
</script>

<template>
  <UModal
    v-model:open="open"
    :title="job ? `Job #${job.id} - ${formatDateTime(job.started_at)}` : ''"
    :ui="{ content: 'w-[80vw] max-w-[80vw] h-[80vh] max-h-[80vh]' }"
  >
    <template #body>
      <UTabs
        v-if="job"
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
                <UBadge :label="row.original.level" :color="eventColor(row.original.level)" variant="subtle" />
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
