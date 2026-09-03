<script setup>
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { useToast } from '@nuxt/ui/composables'
import { createSchedule, deleteSchedule, getSchedules, updateSchedule } from '@/api/schedules'
import SortableColumnHeader from '@/components/SortableColumnHeader.vue'
import { useAuthStore } from '@/stores/auth'
import { formatDateTime } from '@/utils/datetime'

defineOptions({ name: 'JobSchedulerPage' })

const toast = useToast()
const authStore = useAuthStore()

const targetInfo = {
  all: { label: 'All jobs' },
  housekeeping: { label: 'Housekeeping' },
  becs: { label: 'BECS' },
  lime: { label: 'Lime' },
  netbox: { label: 'Netbox' },
  dns: { label: 'DNS' },
  icinga: { label: 'Icinga' },
  librenms: { label: 'LibreNMS' },
  oxidized: { label: 'Oxidized' },
  prometheus: { label: 'Prometheus' },
  'device-sync': { label: 'Device sync' },
}

const cronPresets = [
  { label: 'Every 5 minutes', value: '*/5 * * * *' },
  { label: 'Every 15 minutes', value: '*/15 * * * *' },
  { label: 'Every 30 minutes', value: '*/30 * * * *' },
  { label: 'Hourly', value: '0 * * * *' },
  { label: 'Daily at 02:00', value: '0 2 * * *' },
  { label: 'Weekly (Sunday 02:00)', value: '0 2 * * 0' },
  { label: 'Custom', value: 'custom' },
]

const cronLabelByValue = Object.fromEntries(
  cronPresets.filter((p) => p.value !== 'custom').map((p) => [p.value, p.label]),
)

const targetItems = Object.entries(targetInfo).map(([value, info]) => ({
  label: info.label,
  value,
}))

const schedules = ref([])
const loading = ref(true)
const error = ref(null)
const globalFilter = ref('')
const sorting = ref([{ id: 'name', desc: false }])

const columns = [
  { id: 'actions', header: '' },
  { accessorKey: 'name', header: 'Name' },
  { accessorKey: 'target', header: 'Job' },
  { accessorKey: 'cron', header: 'Schedule' },
  { id: 'enabled', header: 'Enabled' },
  { id: 'last_run_at', header: 'Last run' },
  { id: 'next_run_at', header: 'Next run' },
  { accessorKey: 'last_error', header: 'Last error' },
]

const emptyForm = () => ({
  name: '',
  target: 'all',
  cron: '0 2 * * *',
  enabled: true,
})

const dialog = ref(false)
const form = ref(emptyForm())
const editingId = ref(null)
const submitted = ref(false)
const saving = ref(false)
const deleteDialog = ref(false)
const deleting = ref(false)
const customCron = ref(false)

const canWrite = computed(() => authStore.canWrite)
const isCreate = computed(() => editingId.value === null)

const cronPreset = computed({
  get() {
    if (customCron.value) {
      return 'custom'
    }
    return cronLabelByValue[form.value.cron] ? form.value.cron : 'custom'
  },
  set(value) {
    if (value === 'custom') {
      customCron.value = true
      return
    }
    customCron.value = false
    form.value.cron = value
  },
})

function targetLabel(target) {
  return targetInfo[target]?.label ?? target
}

function scheduleLabel(cron) {
  return cronLabelByValue[cron] ?? cron
}

function formatRun(value) {
  if (!value) {
    return '—'
  }
  return formatDateTime(value)
}

let pollTimer = null

function loadSchedules() {
  loading.value = true
  error.value = null
  getSchedules()
    .then((data) => {
      schedules.value = data ?? []
    })
    .catch(() => {
      error.value = 'Failed to load schedules.'
    })
    .finally(() => {
      loading.value = false
    })
}

function refreshSchedules() {
  getSchedules()
    .then((data) => {
      schedules.value = data ?? []
    })
    .catch(() => {
      // Keep the last good table; this is a background poll.
    })
}

function openNew() {
  editingId.value = null
  form.value = emptyForm()
  customCron.value = false
  submitted.value = false
  dialog.value = true
}

function editSchedule(row) {
  editingId.value = row.id
  form.value = {
    name: row.name ?? '',
    target: row.target ?? 'all',
    cron: row.cron ?? '',
    enabled: !!row.enabled,
  }
  customCron.value = !cronLabelByValue[row.cron]
  submitted.value = false
  dialog.value = true
}

function hideDialog() {
  dialog.value = false
  submitted.value = false
}

function save() {
  submitted.value = true
  if (!form.value.name?.trim() || !form.value.target || !form.value.cron?.trim()) {
    return
  }

  saving.value = true
  const payload = {
    name: form.value.name.trim(),
    target: form.value.target,
    cron: form.value.cron.trim(),
    enabled: form.value.enabled,
  }
  const request = isCreate.value
    ? createSchedule(payload)
    : updateSchedule(editingId.value, payload)
  request
    .then(() => {
      toast.add({
        color: 'success',
        title: 'Successful',
        description: isCreate.value ? 'Schedule created' : 'Schedule updated',
        duration: 3000,
      })
      dialog.value = false
      loadSchedules()
    })
    .catch((err) => {
      toast.add({
        color: 'error',
        title: 'Error',
        description: err.response?.data?.error ?? 'Failed to save schedule.',
        duration: 3000,
      })
    })
    .finally(() => {
      saving.value = false
    })
}

function confirmDelete() {
  deleteDialog.value = true
}

function performDelete() {
  if (!editingId.value) {
    return
  }
  deleting.value = true
  deleteSchedule(editingId.value)
    .then(() => {
      deleteDialog.value = false
      dialog.value = false
      toast.add({
        color: 'success',
        title: 'Successful',
        description: 'Schedule deleted',
        duration: 3000,
      })
      loadSchedules()
    })
    .catch((err) => {
      toast.add({
        color: 'error',
        title: 'Error',
        description: err.response?.data?.error ?? 'Failed to delete schedule.',
        duration: 3000,
      })
    })
    .finally(() => {
      deleting.value = false
    })
}

onMounted(() => {
  loadSchedules()
  pollTimer = setInterval(refreshSchedules, 15000)
})

onUnmounted(() => {
  if (pollTimer) {
    clearInterval(pollTimer)
  }
})
</script>

<template>
  <div class="card">
    <div class="flex flex-wrap gap-2 items-center justify-between mb-4">
      <div class="flex items-center gap-2">
        <h4 class="m-0">Scheduler</h4>
        <UButton
          v-if="canWrite"
          label="New"
          icon="i-lucide-plus"
          color="neutral"
          size="sm"
          @click="openNew"
        />
      </div>
      <UInput v-model="globalFilter" icon="i-lucide-search" placeholder="Search..." />
    </div>

    <p class="text-muted-color mb-4">
      Periodic jobs that trigger one sync target, housekeeping (trims old job history in the
      database), or all enabled syncs in sequence. Housekeeping is not part of "All jobs" and does
      not run unless you schedule it. Times are Europe/Stockholm.
    </p>

    <UTable
      v-model:sorting="sorting"
      v-model:global-filter="globalFilter"
      :data="schedules"
      :columns="columns"
      :loading="loading"
      :empty="error ?? 'No schedules yet.'"
      :virtualize="{ estimateSize: 46 }"
      class="max-h-[calc(100vh-380px)]"
    >
      <template #name-header="{ column }">
        <SortableColumnHeader :column="column" label="Name" />
      </template>
      <template #target-header="{ column }">
        <SortableColumnHeader :column="column" label="Job" />
      </template>
      <template #target-cell="{ row }">
        {{ targetLabel(row.original.target) }}
      </template>
      <template #cron-header="{ column }">
        <SortableColumnHeader :column="column" label="Schedule" />
      </template>
      <template #cron-cell="{ row }">
        <div>
          <div>{{ scheduleLabel(row.original.cron) }}</div>
          <div
            v-if="scheduleLabel(row.original.cron) !== row.original.cron"
            class="text-xs text-muted-color"
          >
            {{ row.original.cron }}
          </div>
        </div>
      </template>
      <template #enabled-cell="{ row }">
        <UBadge
          :label="row.original.enabled ? 'Yes' : 'No'"
          :color="row.original.enabled ? 'success' : 'neutral'"
          variant="subtle"
        />
      </template>
      <template #last_run_at-cell="{ row }">
        {{ formatRun(row.original.last_run_at) }}
      </template>
      <template #next_run_at-cell="{ row }">
        {{ formatRun(row.original.next_run_at) }}
      </template>
      <template #last_error-cell="{ row }">
        <span v-if="row.original.last_error" class="text-red-500 text-sm">{{
          row.original.last_error
        }}</span>
        <span v-else class="text-muted-color">—</span>
      </template>
      <template #actions-cell="{ row }">
        <UButton
          v-if="canWrite"
          icon="i-lucide-pencil"
          variant="outline"
          color="neutral"
          size="sm"
          @click="editSchedule(row.original)"
        />
      </template>
    </UTable>
  </div>

  <UModal
    v-model:open="dialog"
    :title="isCreate ? 'New schedule' : 'Edit schedule'"
    :ui="{ content: 'sm:max-w-md' }"
  >
    <template #body>
      <div class="flex flex-col gap-6">
        <div>
          <label for="sched-name" class="block font-bold mb-3">Name</label>
          <UInput
            id="sched-name"
            v-model.trim="form.name"
            :color="submitted && !form.name?.trim() ? 'error' : undefined"
            :highlight="submitted && !form.name?.trim()"
            autofocus
            class="w-full"
          />
          <small v-if="submitted && !form.name?.trim()" class="text-red-500"
            >Name is required.</small
          >
        </div>
        <div>
          <label for="sched-target" class="block font-bold mb-3">Job</label>
          <USelect
            id="sched-target"
            v-model="form.target"
            :items="targetItems"
            value-key="value"
            label-key="label"
            class="w-full"
          />
          <small class="text-muted-color">
            All jobs runs every enabled source then destination, same as Sync all. Housekeeping is
            not included in All jobs.
          </small>
        </div>
        <div>
          <label for="sched-preset" class="block font-bold mb-3">Repeat</label>
          <USelect
            id="sched-preset"
            v-model="cronPreset"
            :items="cronPresets"
            value-key="value"
            label-key="label"
            class="w-full"
          />
        </div>
        <div>
          <label for="sched-cron" class="block font-bold mb-3">Cron expression</label>
          <UInput
            id="sched-cron"
            v-model.trim="form.cron"
            :disabled="cronPreset !== 'custom'"
            :color="submitted && !form.cron?.trim() ? 'error' : undefined"
            class="w-full font-mono"
          />
          <small class="text-muted-color">
            Five fields: minute hour day-of-month month day-of-week. Descriptors like @hourly and
            @every 15m also work.
          </small>
        </div>
        <div class="flex items-center gap-3">
          <USwitch v-model="form.enabled" id="sched-enabled" />
          <label for="sched-enabled" class="font-bold">Enabled</label>
        </div>
      </div>
    </template>

    <template #footer>
      <div class="flex w-full justify-between">
        <UButton
          v-if="!isCreate && canWrite"
          label="Delete"
          icon="i-lucide-trash-2"
          variant="outline"
          color="error"
          @click="confirmDelete"
        />
        <div class="flex gap-2 ms-auto">
          <UButton label="Cancel" icon="i-lucide-x" variant="ghost" @click="hideDialog" />
          <UButton
            v-if="canWrite"
            label="Save"
            icon="i-lucide-check"
            :loading="saving"
            @click="save"
          />
        </div>
      </div>
    </template>
  </UModal>

  <UModal v-model:open="deleteDialog" title="Delete schedule" :ui="{ content: 'sm:max-w-sm' }">
    <template #body>
      <p>
        Delete schedule <strong>{{ form.name || 'this schedule' }}</strong
        >? Jobs already running are not cancelled.
      </p>
    </template>
    <template #footer>
      <UButton label="Cancel" icon="i-lucide-x" variant="ghost" @click="deleteDialog = false" />
      <UButton
        label="Delete"
        icon="i-lucide-trash-2"
        color="error"
        :loading="deleting"
        @click="performDelete"
      />
    </template>
  </UModal>
</template>
