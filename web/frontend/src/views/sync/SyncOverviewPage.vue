<script setup>
import { useToast } from '@nuxt/ui/composables'
import { computed, onMounted, onUnmounted, reactive, ref, watch } from 'vue'
import { getJobs, getSyncTargets, triggerSync, triggerSyncAll } from '@/api/jobs'
import { useAuthStore } from '@/stores/auth'
import { formatDateTime } from '@/utils/datetime'

const toast = useToast()
const authStore = useAuthStore()

const targetInfo = {
  becs: { label: 'BECS', icon: 'i-lucide-database', section: 'source' },
  lime: { label: 'Lime', icon: 'i-lucide-briefcase', section: 'source' },
  netbox: { label: 'Netbox', icon: 'i-lucide-network', section: 'source' },

  dns: { label: 'DNS', icon: 'i-lucide-globe', section: 'destination' },
  icinga: { label: 'Icinga', icon: 'i-lucide-heart', section: 'destination' },
  librenms: { label: 'LibreNMS', icon: 'i-lucide-line-chart', section: 'destination' },
  oxidized: { label: 'Oxidized', icon: 'i-lucide-save', section: 'destination' },
  prometheus: { label: 'Prometheus', icon: 'i-lucide-gauge', section: 'destination' },

  housekeeping: { label: 'Housekeeping', icon: 'i-lucide-trash-2', section: 'maintenance' },
}

const targets = ref([])
const loading = ref(true)
const loadError = ref(false)
const syncing = reactive({})
const jobs = ref([])
// Set to the triggered batch job's numeric ID while "Sync all" is
// in flight, cleared once that job's tasks have all finished (see the
// jobs watcher below) - syncingAll derives its running/done state from
// job history rather than a manual wait loop.
const syncingAllJobId = ref(null)

// Poll job history while the page is open so a tile's "Running…" status
// flips to done without needing a manual refresh - relevant both for a
// job triggered from another tab/user and for the batch this tab kicked
// off via "Sync all".
let pollTimer = null

// getJobs() returns newest-first Jobs, each with its JobTasks embedded, so
// the first task seen per target (flattened across jobs in that order) is
// already the most recent - no need to sort/compare timestamps here.
const latestTaskByTarget = computed(() => {
  const map = {}
  for (const job of jobs.value) {
    for (const task of job.tasks ?? []) {
      if (!map[task.target]) {
        map[task.target] = { ...task, triggered_by: job.triggered_by }
      }
    }
  }
  return map
})

const tiles = computed(() =>
  targets.value.map((target) => ({
    target,
    label: targetInfo[target]?.label ?? target,
    icon: targetInfo[target]?.icon ?? 'i-lucide-refresh-cw',
    section: targetInfo[target]?.section ?? 'destination',
    lastTask: latestTaskByTarget.value[target],
  })),
)

const sourceTiles = computed(() => tiles.value.filter((tile) => tile.section === 'source'))
const destinationTiles = computed(() =>
  tiles.value.filter((tile) => tile.section === 'destination'),
)
const maintenanceTiles = computed(() =>
  tiles.value.filter((tile) => tile.section === 'maintenance'),
)
const syncableTargets = computed(() => targets.value.filter((t) => t !== 'housekeeping'))

function lastTaskStatus(task) {
  if (!task) {
    return null
  }
  if (!task.finished_at) {
    return { text: 'Running…', class: 'text-muted' }
  }
  const when = formatDateTime(task.finished_at)
  if (task.exit_code === 0) {
    return { text: `✓ ${when}`, class: 'text-green-600' }
  }
  return { text: `✗ failed ${when}`, class: 'text-red-500' }
}

// A tile is "running" purely from job history (lastTask has no
// finished_at yet) - this reflects the server-enforced one-dispatch-per-
// target lock (worker.RemoteManager.running), including a task triggered
// from another tab or by another user, not just this tab's own in-flight
// request.
function isRunning(tile) {
  return !!tile.lastTask && !tile.lastTask.finished_at
}

function actionLabel(tile) {
  if (isRunning(tile)) {
    return 'Running…'
  }
  return tile.target === 'housekeeping' ? 'Run' : 'Sync'
}

function queuedDescription(target) {
  const label = targetInfo[target]?.label ?? target
  return target === 'housekeeping' ? `${label} has been queued.` : `${label} sync has been queued.`
}

function loadTargets() {
  loading.value = true
  loadError.value = false
  getSyncTargets()
    .then((data) => {
      targets.value = data
    })
    .catch(() => {
      loadError.value = true
    })
    .finally(() => {
      loading.value = false
    })
}

function loadJobs() {
  return getJobs()
    .then((data) => {
      jobs.value = data ?? []
    })
    .catch(() => {
      // Non-critical for this page - the Sync buttons still work without
      // job history, just without the last-run status line.
    })
}

function sync(target) {
  syncing[target] = true
  triggerSync(target)
    .then(() => {
      toast.add({
        color: 'success',
        title: target === 'housekeeping' ? 'Housekeeping queued' : 'Sync queued',
        description: queuedDescription(target),
        duration: 3000,
      })
      loadJobs()
    })
    .catch((err) => {
      const alreadyRunning = err.response?.status === 409
      toast.add({
        color: 'error',
        title: alreadyRunning ? 'Sync already running' : 'Sync failed',
        description:
          err.response?.data?.error ??
          `Failed to queue ${targetInfo[target]?.label ?? target} sync.`,
        duration: 3000,
      })
      loadJobs()
    })
    .finally(() => {
      syncing[target] = false
    })
}

// syncingAll is true from the moment "Sync all" is clicked until the batch
// job it triggered shows every task finished in job history - derived from
// polled job state rather than a manual client-side wait loop, since the
// server now dispatches every target concurrently as subjobs of one Job
// (worker.RemoteManager.StartJob) instead of this page triggering and
// awaiting them one at a time.
const syncingAll = computed(() => {
  if (!syncingAllJobId.value) {
    return false
  }
  const job = jobs.value.find((j) => j.id === syncingAllJobId.value)
  return !job || !job.finished_at
})

async function syncAll() {
  if (syncingAll.value) {
    return
  }
  try {
    const { job_id: jobId } = await triggerSyncAll()
    syncingAllJobId.value = jobId
    toast.add({
      color: 'success',
      title: 'Sync all queued',
      description: 'All enabled targets have been queued.',
      duration: 3000,
    })
  } catch (err) {
    toast.add({
      color: 'error',
      title: 'Sync all failed',
      description: err.response?.data?.error ?? 'Failed to queue sync all.',
      duration: 4000,
    })
    return
  }
  await loadJobs()
}

// Fires once the batch job "Sync all" triggered shows every task finished
// in a poll - reports the outcome and clears syncingAllJobId, the
// completion signal syncingAll above watches for.
watch(jobs, (newJobs) => {
  if (!syncingAllJobId.value) {
    return
  }
  const job = newJobs.find((j) => j.id === syncingAllJobId.value)
  if (!job?.finished_at) {
    return
  }
  const tasks = job.tasks ?? []
  const failed = tasks
    .filter((task) => task.exit_code !== 0)
    .map((task) => targetInfo[task.target]?.label ?? task.target)

  if (failed.length === 0) {
    toast.add({
      color: 'success',
      title: 'Sync all complete',
      description: `${tasks.length} target(s) synced successfully.`,
      duration: 4000,
    })
  } else {
    toast.add({
      color: 'error',
      title: 'Sync all finished with errors',
      description: `Failed: ${failed.join(', ')}.`,
      duration: 5000,
    })
  }
  syncingAllJobId.value = null
})

onMounted(() => {
  loadTargets()
  loadJobs()
  pollTimer = setInterval(loadJobs, 3000)
})

onUnmounted(() => {
  if (pollTimer) {
    clearInterval(pollTimer)
  }
})
</script>

<template>
  <div class="card">
    <div class="flex items-center justify-between gap-4 mb-4">
      <div class="font-semibold text-xl">Job overview</div>
      <UButton
        v-if="authStore.canWrite"
        label="Sync all"
        icon="i-lucide-refresh-cw"
        :loading="syncingAll"
        :disabled="loading || syncableTargets.length === 0"
        @click="syncAll"
      />
    </div>

    <UAlert v-if="loadError" color="error" variant="subtle" title="Failed to load sync targets." />

    <div v-if="loading" class="flex justify-center p-4">
      <UIcon name="i-lucide-loader-2" class="size-8 animate-spin" />
    </div>

    <template v-else>
      <template v-if="sourceTiles.length">
        <div class="font-semibold text-lg mb-3">Sources</div>
        <div class="grid grid-cols-12 gap-4 mb-6">
          <div
            v-for="tile in sourceTiles"
            :key="tile.target"
            class="col-span-12 sm:col-span-6 lg:col-span-4"
          >
            <div class="border border-default rounded-lg p-4 flex flex-col gap-2">
              <div class="flex items-center justify-between gap-4">
                <div class="flex items-center gap-3">
                  <UIcon :name="tile.icon" class="size-6" />
                  <span class="font-medium">{{ tile.label }}</span>
                </div>
                <UButton
                  v-if="authStore.canWrite"
                  :label="actionLabel(tile)"
                  icon="i-lucide-refresh-cw"
                  :loading="syncing[tile.target] || isRunning(tile)"
                  :disabled="isRunning(tile) || syncingAll"
                  @click="sync(tile.target)"
                />
              </div>
              <div
                v-if="lastTaskStatus(tile.lastTask)"
                class="text-xs"
                :class="lastTaskStatus(tile.lastTask).class"
              >
                {{ lastTaskStatus(tile.lastTask).text }}
              </div>
            </div>
          </div>
        </div>
      </template>

      <template v-if="destinationTiles.length">
        <div class="font-semibold text-lg mb-3">Destinations</div>
        <div class="grid grid-cols-12 gap-4">
          <div
            v-for="tile in destinationTiles"
            :key="tile.target"
            class="col-span-12 sm:col-span-6 lg:col-span-4"
          >
            <div class="border border-default rounded-lg p-4 flex flex-col gap-2">
              <div class="flex items-center justify-between gap-4">
                <div class="flex items-center gap-3">
                  <UIcon :name="tile.icon" class="size-6" />
                  <span class="font-medium">{{ tile.label }}</span>
                </div>
                <UButton
                  v-if="authStore.canWrite"
                  :label="actionLabel(tile)"
                  icon="i-lucide-refresh-cw"
                  :loading="syncing[tile.target] || isRunning(tile)"
                  :disabled="isRunning(tile) || syncingAll"
                  @click="sync(tile.target)"
                />
              </div>
              <div
                v-if="lastTaskStatus(tile.lastTask)"
                class="text-xs"
                :class="lastTaskStatus(tile.lastTask).class"
              >
                {{ lastTaskStatus(tile.lastTask).text }}
              </div>
            </div>
          </div>
        </div>
      </template>

      <template v-if="maintenanceTiles.length">
        <div class="font-semibold text-lg mb-3 mt-6">Maintenance</div>
        <div class="grid grid-cols-12 gap-4">
          <div
            v-for="tile in maintenanceTiles"
            :key="tile.target"
            class="col-span-12 sm:col-span-6 lg:col-span-4"
          >
            <div class="border border-default rounded-lg p-4 flex flex-col gap-2">
              <div class="flex items-center justify-between gap-4">
                <div class="flex items-center gap-3">
                  <UIcon :name="tile.icon" class="size-6" />
                  <span class="font-medium">{{ tile.label }}</span>
                </div>
                <UButton
                  v-if="authStore.canWrite"
                  :label="actionLabel(tile)"
                  icon="i-lucide-trash-2"
                  :loading="syncing[tile.target] || isRunning(tile)"
                  :disabled="isRunning(tile) || syncingAll"
                  @click="sync(tile.target)"
                />
              </div>
              <div
                v-if="lastTaskStatus(tile.lastTask)"
                class="text-xs"
                :class="lastTaskStatus(tile.lastTask).class"
              >
                {{ lastTaskStatus(tile.lastTask).text }}
              </div>
            </div>
          </div>
        </div>
      </template>

      <div class="font-semibold text-lg mb-3 mt-6">Troubleshooting</div>
      <div class="border border-default rounded-lg p-4 flex items-center justify-between gap-4">
        <div>
          <div class="font-medium">Which worker nodes are up?</div>
          <div class="text-muted text-sm">
            See connected worker nodes, the roles each one handles, and recent job history.
          </div>
        </div>
        <RouterLink to="/sync/status">
          <UButton label="Job status" icon="i-lucide-list-checks" />
        </RouterLink>
      </div>
    </template>
  </div>
</template>
