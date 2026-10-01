<script setup>
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { useToast } from '@nuxt/ui/composables'
import { createSchedule, deleteSchedule, getSchedules, updateSchedule } from '@/api/schedules'
import SearchInput from '@/components/SearchInput.vue'
import SortableColumnHeader from '@/components/SortableColumnHeader.vue'
import { useConfirm } from '@/composables/useConfirm'
import { useAuthStore } from '@/stores/auth'
import { formatDateTime } from '@/utils/datetime'
import { useSearch, valuesText } from '@/utils/search'

defineOptions({ name: 'JobSchedulerPage' })

const toast = useToast()
const authStore = useAuthStore()
const { confirmDelete } = useConfirm()

const targetInfo = {
  all: { label: 'All jobs' },
  housekeeping: { label: 'Housekeeping' },
  becs: { label: 'BECS' },
  lime: { label: 'Lime' },
  netbox: { label: 'Netbox' },
  'netbox-delta': { label: 'Netbox changes' },
  dns: { label: 'DNS' },
  certs: { label: 'Certificates' },
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
const sorting = ref([{ id: 'name', desc: false }])

const columns = [
  { id: 'actions', header: '' },
  { accessorKey: 'name', header: 'Name' },
  { accessorKey: 'target', header: 'Jobs' },
  { accessorKey: 'cron', header: 'Schedule' },
  { id: 'enabled', header: 'Enabled' },
  { id: 'last_run_at', header: 'Last run' },
  { id: 'next_run_at', header: 'Next run' },
  { accessorKey: 'last_error', header: 'Last error' },
]

const emptyForm = () => ({
  name: '',
  targets: ['all'],
  cron: '0 2 * * *',
  enabled: true,
})

const dialog = ref(false)
const form = ref(emptyForm())
const editingId = ref(null)
const submitted = ref(false)
const saving = ref(false)
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

function splitTargets(target) {
  return String(target ?? '')
    .split(',')
    .map((part) => part.trim())
    .filter(Boolean)
}

function targetLabel(target) {
  const parts = splitTargets(target)
  if (parts.length === 0) {
    return '—'
  }
  return parts.map((part) => targetInfo[part]?.label ?? part).join(', ')
}

// All jobs already means every enabled sync, so it is stored and run on
// its own. Picking it clears the other jobs; picking another job drops it.
function setTargets(next) {
  const values = Array.isArray(next) ? next.map(String) : []
  const hadAll = form.value.targets.includes('all')
  const hasAll = values.includes('all')
  if (hasAll && !hadAll) {
    form.value.targets = ['all']
    return
  }
  if (hasAll && values.length > 1) {
    form.value.targets = values.filter((target) => target !== 'all')
    return
  }
  form.value.targets = values
}

function scheduleLabel(cron) {
  return cronLabelByValue[cron] ?? cron
}

const { search, filtered } = useSearch(schedules, (row) =>
  valuesText(
    row.name,
    targetLabel(row.target),
    scheduleLabel(row.cron),
    row.cron,
    row.enabled ? 'Yes' : 'No',
    formatRun(row.last_run_at),
    formatRun(row.next_run_at),
    row.last_error,
  ),
)

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
    targets: splitTargets(row.target),
    cron: row.cron ?? '',
    enabled: !!row.enabled,
  }
  customCron.value = !cronLabelByValue[row.cron]
  submitted.value = false
  dialog.value = true
}

function save() {
  submitted.value = true
  if (!form.value.name?.trim() || !form.value.targets?.length || !form.value.cron?.trim()) {
    return
  }

  saving.value = true
  const payload = {
    name: form.value.name.trim(),
    target: form.value.targets.join(','),
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

async function performDelete() {
  if (!editingId.value) {
    return
  }
  const ok = await confirmDelete(
    `schedule ${form.value.name || 'this schedule'}`,
    'Jobs already running are not cancelled.',
  )
  if (!ok) return
  deleting.value = true
  deleteSchedule(editingId.value)
    .then(() => {
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
      <SearchInput v-model="search" />
    </div>

    <p class="text-muted-color mb-4">
      Periodic jobs that trigger the jobs you pick, one at a time. All jobs runs every enabled
      source then destination, same as Sync all. Housekeeping trims old job history and is not part
      of All jobs. Times are Europe/Stockholm.
    </p>

    <UTable
      v-model:sorting="sorting"
      :data="filtered"
      :columns="columns"
      :loading="loading"
      :empty="error || (search && schedules.length ? 'Nothing matches the search.' : 'No schedules yet.')"
      :virtualize="{ estimateSize: 46 }"
      class="max-h-[calc(100vh-380px)]"
    >
      <template #name-header="{ column }">
        <SortableColumnHeader :column="column" label="Name" />
      </template>
      <template #target-header="{ column }">
        <SortableColumnHeader :column="column" label="Jobs" />
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

  <FormModal
    v-model:open="dialog"
    :source="form"
    :title="isCreate ? 'New schedule' : 'Edit schedule'"
  >
    <template #body>
      <div class="flex flex-col gap-4">
        <UFormField label="Name">
          <UInput
            v-model.trim="form.name"
            :color="submitted && !form.name?.trim() ? 'error' : undefined"
            :highlight="submitted && !form.name?.trim()"
            autofocus
            class="w-full"
          />
          <small v-if="submitted && !form.name?.trim()" class="text-red-500">Name is required.</small>
        </UFormField>
        <UFormField
          label="Jobs"
          hint="Pick one or more. They run one at a time, sources before destinations. All jobs is every enabled sync and is not combined with other jobs. Housekeeping is not included in All jobs."
        >
          <USelectMenu
            :model-value="form.targets"
            :items="targetItems"
            value-key="value"
            label-key="label"
            multiple
            placeholder="Select jobs"
            class="w-full"
            @update:model-value="setTargets"
          />
          <small v-if="submitted && !form.targets?.length" class="text-red-500">
            Select at least one job.
          </small>
        </UFormField>
        <UFormField label="Repeat">
          <USelect
            v-model="cronPreset"
            :items="cronPresets"
            value-key="value"
            label-key="label"
            class="w-full"
          />
        </UFormField>
        <UFormField
          label="Cron expression"
          hint="Five fields: minute hour day-of-month month day-of-week. Descriptors like @hourly and @every 15m also work."
        >
          <UInput
            v-model.trim="form.cron"
            :disabled="cronPreset !== 'custom'"
            :color="submitted && !form.cron?.trim() ? 'error' : undefined"
            class="w-full font-mono"
          />
        </UFormField>
        <UFormField label="Enabled">
          <USwitch v-model="form.enabled" />
        </UFormField>
      </div>
    </template>

    <template #footer="{ close }">
      <div class="flex w-full gap-2">
        <UButton
          v-if="!isCreate && canWrite"
          label="Delete"
          icon="i-lucide-trash"
          color="error"
          variant="ghost"
          :loading="deleting"
          @click="performDelete"
        />
        <UButton class="ms-auto" label="Cancel" icon="i-lucide-x" variant="ghost" @click="close" />
        <UButton
          v-if="canWrite"
          label="Save"
          icon="i-lucide-check"
          :loading="saving"
          @click="save"
        />
      </div>
    </template>
  </FormModal>
</template>
