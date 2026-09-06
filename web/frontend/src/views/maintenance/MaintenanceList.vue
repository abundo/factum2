<script setup>
import { computed, onMounted, ref } from 'vue'
import { useToast } from '@nuxt/ui/composables'
import { getConnections } from '@/api/connections'
import { getDevices } from '@/api/devices'
import {
  createMaintenance,
  getMaintenance,
  listMaintenance,
  notifyMaintenance,
  updateMaintenance,
} from '@/api/maintenance'
import { getServices } from '@/api/services'
import { useAuthStore } from '@/stores/auth'

const toast = useToast()
const authStore = useAuthStore()
const rows = ref([])
const devices = ref([])
const connections = ref([])
const services = ref([])
const loading = ref(true)
const createOpen = ref(false)
const saving = ref(false)
const detail = ref(null)

function emptyForm() {
  return {
    title: '',
    description: '',
    device_ids: [],
    fiber_keys: [],
    wavelength_ids: [],
    starts_at: '',
    ends_at: '',
    status: 'planned',
  }
}

const form = ref(emptyForm())

const deviceItems = computed(() => (devices.value ?? []).map((d) => ({ id: d.id, name: d.name })))

const fiberItems = computed(() => {
  const cables = (connections.value ?? []).map((c) => ({
    value: `connection:${c.id}`,
    label: c.name,
  }))
  const dark = (services.value ?? [])
    .filter((s) => s.category === 'LF' || s.category === 'LI')
    .map((s) => ({
      value: `fiber:${s.id}`,
      label: s.company ? `${s.service_id} — ${s.company}` : s.service_id,
    }))
  return [...cables, ...dark]
})

const wavelengthItems = computed(() =>
  (services.value ?? [])
    .filter((s) => s.category === 'VL' || s.category === 'VI')
    .map((s) => ({
      id: s.id,
      name: s.company ? `${s.service_id} — ${s.company}` : s.service_id,
    })),
)

const resourceTypeLabels = {
  device: 'Device',
  connection: 'Fiber',
  fiber: 'Fiber',
  wavelength: 'Wavelength',
  interface: 'Interface',
}

function asIDs(values) {
  return (values ?? [])
    .map((v) => Number(typeof v === 'object' && v != null ? v.id : v))
    .filter((n) => Number.isInteger(n) && n > 0)
}

function asKeys(values) {
  return (values ?? [])
    .map((v) => (typeof v === 'object' && v != null ? v.value : v))
    .filter(Boolean)
}

function resourceSummary(row) {
  const resources = row.resources?.length
    ? row.resources
    : row.resource_type
      ? [{ resource_type: row.resource_type }]
      : []
  const counts = {}
  for (const r of resources) {
    const kind = r.resource_type === 'connection' ? 'fiber' : r.resource_type
    counts[kind] = (counts[kind] ?? 0) + 1
  }
  const order = ['device', 'fiber', 'wavelength', 'interface']
  const plurals = {
    device: ['device', 'devices'],
    fiber: ['fiber', 'fibers'],
    wavelength: ['wavelength', 'wavelengths'],
    interface: ['interface', 'interfaces'],
  }
  const parts = []
  for (const kind of order) {
    const n = counts[kind]
    if (!n) continue
    const [one, many] = plurals[kind] ?? [kind, `${kind}s`]
    parts.push(n === 1 ? `1 ${one}` : `${n} ${many}`)
  }
  return parts.join(', ') || row.resource_type || '—'
}

function load() {
  loading.value = true
  Promise.all([listMaintenance(), getDevices(), getConnections(), getServices()])
    .then(([m, d, c, s]) => {
      rows.value = m ?? []
      devices.value = d ?? []
      connections.value = c ?? []
      services.value = s ?? []
    })
    .finally(() => {
      loading.value = false
    })
}

function openCreate() {
  form.value = emptyForm()
  createOpen.value = true
}

function save() {
  const resources = []
  for (const id of asIDs(form.value.device_ids)) {
    resources.push({ resource_type: 'device', resource_id: id })
  }
  for (const key of asKeys(form.value.fiber_keys)) {
    const [kind, rawId] = String(key).split(':')
    const id = Number(rawId)
    if ((kind === 'connection' || kind === 'fiber') && id > 0) {
      resources.push({ resource_type: kind, resource_id: id })
    }
  }
  for (const id of asIDs(form.value.wavelength_ids)) {
    resources.push({ resource_type: 'wavelength', resource_id: id })
  }
  if (!form.value.title.trim()) {
    toast.add({ color: 'error', title: 'Title is required' })
    return
  }
  if (!form.value.starts_at) {
    toast.add({ color: 'error', title: 'Start time is required' })
    return
  }
  if (!resources.length) {
    toast.add({ color: 'error', title: 'Select at least one fiber, wavelength, or device' })
    return
  }
  const payload = {
    title: form.value.title,
    description: form.value.description,
    status: form.value.status,
    resources,
    starts_at: new Date(form.value.starts_at).toISOString(),
    ends_at: form.value.ends_at ? new Date(form.value.ends_at).toISOString() : undefined,
  }
  saving.value = true
  createMaintenance(payload)
    .then(() => {
      createOpen.value = false
      load()
    })
    .catch((err) => {
      toast.add({ color: 'error', title: 'Create failed', description: err?.response?.data?.error })
    })
    .finally(() => {
      saving.value = false
    })
}

function open(row) {
  getMaintenance(row.id).then((data) => {
    detail.value = data
  })
}

function notify() {
  notifyMaintenance(detail.value.window.id)
    .then((r) => {
      toast.add({ color: 'success', title: `Sent ${r.sent}, failed ${r.failed}` })
      open(detail.value.window)
    })
    .catch((err) => {
      toast.add({ color: 'error', title: 'Notify failed', description: err?.response?.data?.error })
    })
}

function setStatus(status) {
  updateMaintenance(detail.value.window.id, { status }).then(() => open(detail.value.window))
}

onMounted(load)
</script>

<template>
  <div class="card">
    <div class="flex justify-between items-center mb-4">
      <h4 class="m-0">Maintenance</h4>
      <UButton
        v-if="authStore.canWrite"
        label="New window"
        icon="i-lucide-plus"
        @click="openCreate"
      />
    </div>
    <UTable
      :data="rows"
      :loading="loading"
      :columns="[
        { accessorKey: 'title', header: 'Title' },
        { id: 'resource', header: 'Resources' },
        { accessorKey: 'starts_at', header: 'Starts' },
        { accessorKey: 'status', header: 'Status' },
        { id: 'actions', header: '' },
      ]"
    >
      <template #resource-cell="{ row }">
        {{ resourceSummary(row.original) }}
      </template>
      <template #actions-cell="{ row }">
        <UButton
          size="sm"
          variant="outline"
          color="neutral"
          label="Open"
          @click="open(row.original)"
        />
      </template>
    </UTable>
  </div>

  <UModal v-model:open="createOpen" title="New maintenance window" :ui="{ content: 'sm:max-w-lg' }">
    <template #body>
      <div class="flex flex-col gap-3">
        <UInput v-model="form.title" placeholder="Title" />
        <UTextarea v-model="form.description" placeholder="Description" />
        <div>
          <div class="font-bold mb-1">Devices</div>
          <USelectMenu
            v-model="form.device_ids"
            :items="deviceItems"
            value-key="id"
            label-key="name"
            multiple
            placeholder="Select devices"
            class="w-full"
          />
        </div>
        <div>
          <div class="font-bold mb-1">Fibers</div>
          <USelectMenu
            v-model="form.fiber_keys"
            :items="fiberItems"
            value-key="value"
            label-key="label"
            multiple
            placeholder="Select cables or dark-fiber services"
            class="w-full"
          />
        </div>
        <div>
          <div class="font-bold mb-1">Wavelengths</div>
          <USelectMenu
            v-model="form.wavelength_ids"
            :items="wavelengthItems"
            value-key="id"
            label-key="name"
            multiple
            placeholder="Select wavelength services"
            class="w-full"
          />
        </div>
        <label>Starts <UInput v-model="form.starts_at" type="datetime-local" /></label>
        <label>Ends <UInput v-model="form.ends_at" type="datetime-local" /></label>
      </div>
    </template>
    <template #footer>
      <UButton label="Create" :loading="saving" @click="save" />
    </template>
  </UModal>

  <UModal
    :open="!!detail"
    title="Maintenance"
    @update:open="
      (v) => {
        if (!v) detail = null
      }
    "
  >
    <template #body>
      <div v-if="detail" class="flex flex-col gap-4">
        <div>
          <div class="font-bold">{{ detail.window.title }}</div>
          <div class="text-muted-color">
            {{ detail.window.status }} · {{ detail.window.starts_at }}
          </div>
          <p>{{ detail.window.description }}</p>
          <ul v-if="(detail.resources ?? []).length" class="mt-2 text-sm">
            <li v-for="r in detail.resources" :key="`${r.resource_type}-${r.resource_id}`">
              {{ resourceTypeLabels[r.resource_type] || r.resource_type }}:
              {{ r.label }}
            </li>
          </ul>
        </div>
        <div>
          <div class="font-bold mb-2">Affected services</div>
          <div v-if="!(detail.impact ?? []).length" class="text-muted-color">
            None (or untraced)
          </div>
          <ul>
            <li v-for="s in detail.impact ?? []" :key="s.id">
              {{ s.service_id }} — {{ s.customer }}
            </li>
          </ul>
        </div>
        <div class="flex flex-wrap gap-2">
          <UButton label="Notify customers" @click="notify" />
          <UButton label="In progress" variant="outline" @click="setStatus('in_progress')" />
          <UButton label="Complete" variant="outline" @click="setStatus('completed')" />
          <UButton label="Cancel" color="error" variant="outline" @click="setStatus('cancelled')" />
        </div>
      </div>
    </template>
  </UModal>
</template>
