<script setup>
import { onMounted, ref } from 'vue'
import { useToast } from '@nuxt/ui/composables'
import {
  createMaintenance,
  getMaintenance,
  listMaintenance,
  notifyMaintenance,
  updateMaintenance,
} from '@/api/maintenance'
import { getDevices } from '@/api/devices'
import { useAuthStore } from '@/stores/auth'

const toast = useToast()
const authStore = useAuthStore()
const rows = ref([])
const devices = ref([])
const loading = ref(true)
const createOpen = ref(false)
const detail = ref(null)
const form = ref({
  title: '',
  description: '',
  resource_type: 'device',
  resource_id: null,
  starts_at: '',
  ends_at: '',
  status: 'planned',
})

function load() {
  loading.value = true
  Promise.all([listMaintenance(), getDevices()])
    .then(([m, d]) => {
      rows.value = m ?? []
      devices.value = d ?? []
    })
    .finally(() => {
      loading.value = false
    })
}

function save() {
  const payload = {
    ...form.value,
    resource_id: Number(form.value.resource_id),
    starts_at: new Date(form.value.starts_at).toISOString(),
    ends_at: form.value.ends_at ? new Date(form.value.ends_at).toISOString() : undefined,
  }
  createMaintenance(payload)
    .then(() => {
      createOpen.value = false
      load()
    })
    .catch((err) => {
      toast.add({ color: 'error', title: 'Create failed', description: err?.response?.data?.error })
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
      <UButton v-if="authStore.canWrite" label="New window" icon="i-lucide-plus" @click="createOpen = true" />
    </div>
    <UTable
      :data="rows"
      :loading="loading"
      :columns="[
        { accessorKey: 'title', header: 'Title' },
        { accessorKey: 'resource_type', header: 'Resource' },
        { accessorKey: 'starts_at', header: 'Starts' },
        { accessorKey: 'status', header: 'Status' },
        { id: 'actions', header: '' },
      ]"
    >
      <template #actions-cell="{ row }">
        <UButton size="sm" variant="outline" color="neutral" label="Open" @click="open(row.original)" />
      </template>
    </UTable>
  </div>

  <UModal v-model:open="createOpen" title="New maintenance window">
    <template #body>
      <div class="flex flex-col gap-3">
        <UInput v-model="form.title" placeholder="Title" />
        <UTextarea v-model="form.description" placeholder="Description" />
        <USelect
          v-model="form.resource_type"
          :items="[
            { label: 'Device', value: 'device' },
            { label: 'Connection', value: 'connection' },
            { label: 'Interface', value: 'interface' },
          ]"
          value-key="value"
          label-key="label"
        />
        <USelect
          v-if="form.resource_type === 'device'"
          v-model="form.resource_id"
          :items="devices.map((d) => ({ label: d.name, value: d.id }))"
          value-key="value"
          label-key="label"
          placeholder="Device"
        />
        <UInput v-else v-model="form.resource_id" placeholder="Resource ID" />
        <label>Starts <UInput v-model="form.starts_at" type="datetime-local" /></label>
        <label>Ends <UInput v-model="form.ends_at" type="datetime-local" /></label>
      </div>
    </template>
    <template #footer>
      <UButton label="Create" @click="save" />
    </template>
  </UModal>

  <UModal :open="!!detail" title="Maintenance" @update:open="(v) => { if (!v) detail = null }">
    <template #body>
      <div v-if="detail" class="flex flex-col gap-4">
        <div>
          <div class="font-bold">{{ detail.window.title }}</div>
          <div class="text-muted-color">{{ detail.window.status }} · {{ detail.window.starts_at }}</div>
          <p>{{ detail.window.description }}</p>
        </div>
        <div>
          <div class="font-bold mb-2">Affected services</div>
          <div v-if="!(detail.impact ?? []).length" class="text-muted-color">None (or untraced)</div>
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
