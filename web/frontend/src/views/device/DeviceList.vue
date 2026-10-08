<script setup>
import { useToast } from '@nuxt/ui/composables'
import { computed, onMounted, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { createDevice, getDevice, getDeviceImpact, getDevices } from '@/api/devices'
import { getDeviceTypes, getManufacturers, getPlatforms } from '@/api/dcim'
import { getSite, getSites } from '@/api/sites'
import DeviceDetailDialog from '@/components/DeviceDetailDialog.vue'
import SearchInput from '@/components/SearchInput.vue'
import SiteSelector from '@/components/SiteSelector.vue'
import SortableColumnHeader from '@/components/SortableColumnHeader.vue'
import { useAuthStore } from '@/stores/auth'
import { useSearch, valuesText } from '@/utils/search'

const toast = useToast()
const route = useRoute()
const authStore = useAuthStore()

const devices = ref([])
const loading = ref(true)
const error = ref(null)

const sorting = ref([{ id: 'name', desc: false }])
const {
  search: deviceSearch,
  words: deviceWords,
  filtered: filteredDevices,
} = useSearch(devices, (d) =>
  valuesText(
    d.name,
    d.site,
    d.role,
    d.status,
    d.manufacturer,
    d.model_name,
    d.primary_ipv4,
    d.impact?.service_count,
    d.impact?.customer_count,
  ),
)
const deviceEmpty = computed(() => {
  if (error.value) return error.value
  if (deviceWords.value.length && devices.value.length) return 'Nothing matches the search.'
  return 'No devices found.'
})

const columns = [
  { id: 'actions', header: '' },
  { accessorKey: 'name', header: 'Name' },
  { accessorKey: 'site', header: 'Site' },
  { accessorKey: 'role', header: 'Role' },
  { accessorKey: 'status', header: 'Status' },
  { id: 'affected', header: 'Affected' },
  { accessorKey: 'manufacturer', header: 'Manufacturer' },
  { accessorKey: 'model_name', header: 'Model' },
  { accessorKey: 'primary_ipv4', header: 'IPv4' },
]

const createDialog = ref(false)
const createSaving = ref(false)
const siteSelectorVisible = ref(false)
const selectedSite = ref(null)
const sites = ref([])

const detailOpen = ref(false)
const detailId = ref(0)

function onSiteSelected(site) {
  if (!site) {
    createForm.value.site = ''
    createForm.value.site_id = 0
    selectedSite.value = null
    return
  }
  createForm.value.site = site.name || ''
  createForm.value.site_id = site.id || 0
  const cached = site.id && sites.value.find((s) => s.id === site.id)
  selectedSite.value = cached || site
  if (site.id && !cached) {
    getSite(site.id)
      .then((row) => {
        selectedSite.value = row
        if (!sites.value.some((s) => s.id === row.id)) {
          sites.value = [...sites.value, row]
        }
      })
      .catch(() => {})
  }
}

function clearSite() {
  onSiteSelected(null)
}

function formatCoord(v) {
  if (v == null || v === '' || Number(v) === 0) return ''
  const n = Number(v)
  return Number.isFinite(n) ? String(n) : ''
}

function emptyDeviceForm() {
  return {
    name: '',
    device_type_id: undefined,
    platform_id: 0,
    site: '',
    site_id: 0,
    role: '',
    status: 'active',
    comments: '',
    enabled: true,
    vm: false,
    cf_location: '',
    cf_monitor_icinga: false,
    cf_monitor_librenms: false,
    cf_monitor_grafana: false,
    cf_backup_oxidized: false,
    cf_alarm_interfaces: false,
    optical_kind: 'none',
  }
}

const createForm = ref(emptyDeviceForm())
const manufacturers = ref([])
const deviceTypes = ref([])
const platforms = ref([])

const manufacturerById = computed(() => {
  const map = new Map()
  for (const m of manufacturers.value) map.set(m.id, m.name)
  return map
})
const deviceTypeItems = computed(() =>
  deviceTypes.value.map((dt) => ({
    label: `${manufacturerById.value.get(dt.manufacturer_id) || '?'} ${dt.model}${dt.vm ? ' (VM)' : ''}`,
    value: dt.id,
  })),
)
const platformItems = computed(() => [
  { label: 'None', value: 0 },
  ...platforms.value.map((p) => ({ label: `${p.name} (${p.slug})`, value: p.id })),
])
const statusItems = [
  { label: 'active', value: 'active' },
  { label: 'offline', value: 'offline' },
  { label: 'planned', value: 'planned' },
  { label: 'staged', value: 'staged' },
  { label: 'failed', value: 'failed' },
  { label: 'inventory', value: 'inventory' },
  { label: 'decommissioning', value: 'decommissioning' },
]

function loadCatalog() {
  Promise.all([getManufacturers(), getDeviceTypes(), getPlatforms(), getSites()])
    .then(([mfrs, types, plats, siteRows]) => {
      manufacturers.value = mfrs ?? []
      deviceTypes.value = types ?? []
      platforms.value = plats ?? []
      sites.value = siteRows ?? []
    })
    .catch(() => {})
}

function openNew() {
  createForm.value = emptyDeviceForm()
  selectedSite.value = null
  loadCatalog()
  createDialog.value = true
}

watch(
  () => createForm.value.device_type_id,
  (id) => {
    if (!createDialog.value) return
    const dt = deviceTypes.value.find((t) => t.id === id)
    if (!dt) return
    createForm.value.platform_id = dt.platform_id || 0
    createForm.value.vm = !!dt.vm
  },
)

function deviceWritePayload(form) {
  return {
    name: form.name.trim(),
    device_type_id: form.device_type_id,
    platform_id: form.platform_id || 0,
    site_id: form.site_id || 0,
    site: form.site.trim(),
    role: form.role.trim(),
    status: form.status,
    comments: form.comments.trim(),
    enabled: !!form.enabled,
    vm: !!form.vm,
    cf_location: form.cf_location.trim(),
    cf_monitor_icinga: !!form.cf_monitor_icinga,
    cf_monitor_librenms: !!form.cf_monitor_librenms,
    cf_monitor_grafana: !!form.cf_monitor_grafana,
    cf_backup_oxidized: !!form.cf_backup_oxidized,
    cf_alarm_interfaces: !!form.cf_alarm_interfaces,
    optical_kind: !form.optical_kind || form.optical_kind === 'none' ? '' : form.optical_kind,
  }
}

function saveNew() {
  if (!createForm.value.name.trim()) {
    toast.add({ color: 'error', title: 'Name is required' })
    return
  }
  if (!createForm.value.device_type_id) {
    toast.add({ color: 'error', title: 'Device type is required' })
    return
  }
  createSaving.value = true
  createDevice(deviceWritePayload(createForm.value))
    .then(() => {
      createDialog.value = false
      loadDevices()
    })
    .catch((err) => {
      toast.add({
        color: 'error',
        title: 'Create failed',
        description: err?.response?.data?.error,
      })
    })
    .finally(() => {
      createSaving.value = false
    })
}

function loadDevices() {
  loading.value = true
  error.value = null
  getDevices()
    .then((data) => {
      devices.value = data ?? []
      const down = devices.value.filter((d) =>
        ['offline', 'failed', 'decommissioning'].includes((d.status ?? '').toLowerCase()),
      )
      Promise.all(
        down.map((d) =>
          getDeviceImpact(d.id)
            .then((imp) => {
              d.impact = imp
            })
            .catch(() => {}),
        ),
      ).then(() => {
        devices.value = [...devices.value]
      })
    })
    .catch(() => {
      error.value = 'Failed to load devices.'
    })
    .finally(() => {
      loading.value = false
      openFromQuery()
    })
}

function statusColor(status) {
  switch ((status ?? '').toLowerCase()) {
    case 'active':
      return 'success'
    case 'offline':
    case 'failed':
    case 'decommissioning':
      return 'error'
    case 'staged':
    case 'planned':
      return 'warning'
    default:
      return 'neutral'
  }
}

function showDetail(row) {
  const id = Number(row?.id) || 0
  if (!id) return
  detailId.value = id
  detailOpen.value = true
}

function openFromQuery() {
  const id = Number(route.query.id)
  if (!id) return
  const row = devices.value.find((d) => d.id === id)
  if (row) showDetail(row)
  else
    getDevice(id)
      .then((d) => showDetail(d))
      .catch(() => {})
}

watch(
  () => route.query.id,
  () => openFromQuery(),
)

onMounted(() => {
  loadDevices()
})
</script>

<template>
  <div class="card flex min-h-0 flex-1 flex-col overflow-hidden">
    <div class="flex flex-wrap gap-2 items-center justify-between mb-4 shrink-0">
      <div class="flex items-center gap-2">
        <h4 class="m-0">Devices</h4>
        <UButton
          v-if="authStore.canWrite"
          label="New"
          icon="i-lucide-plus"
          color="neutral"
          size="sm"
          @click="openNew"
        />
      </div>
      <SearchInput v-model="deviceSearch" />
    </div>

    <UTable
      v-model:sorting="sorting"
      :data="filteredDevices"
      :columns="columns"
      :loading="loading"
      :empty="deviceEmpty"
      :virtualize="{ estimateSize: 46 }"
      sticky
      class="min-h-0 flex-1"
    >
      <template
        v-for="col in columns.filter((c) => c.id !== 'actions')"
        :key="col.accessorKey"
        #[`${col.accessorKey}-header`]="{ column }"
      >
        <SortableColumnHeader :column="column" :label="col.header" />
      </template>

      <template #actions-cell="{ row }">
        <UButton
          icon="i-lucide-pencil"
          variant="outline"
          size="sm"
          @click="showDetail(row.original)"
        />
      </template>
      <template #status-cell="{ row }">
        <UBadge
          v-if="row.original.status"
          :label="row.original.status"
          :color="statusColor(row.original.status)"
          variant="subtle"
        />
      </template>
      <template #affected-cell="{ row }">
        <span v-if="row.original.impact">
          {{ row.original.impact.service_count }} / {{ row.original.impact.customer_count }}
        </span>
        <span v-else class="text-muted-color">—</span>
      </template>
    </UTable>
  </div>

  <DeviceDetailDialog v-model:open="detailOpen" :device-id="detailId" @changed="loadDevices" />

  <FormModal v-model:open="createDialog" :source="createForm" title="New device">
    <template #body>
      <div class="flex flex-col gap-4">
        <UFormField label="Name">
          <UInput v-model="createForm.name" class="w-full" autofocus />
        </UFormField>
        <UFormField label="Device type">
          <USelect v-model="createForm.device_type_id" :items="deviceTypeItems" class="w-full" />
        </UFormField>
        <UCheckbox v-model="createForm.vm" label="VM" />
        <UFormField label="Platform">
          <USelect v-model="createForm.platform_id" :items="platformItems" class="w-full" />
        </UFormField>
        <UFormField label="Site">
          <div class="flex items-center gap-2">
            <div class="min-w-0 flex-1 text-sm">
              <template v-if="selectedSite">
                <span class="font-medium">{{ selectedSite.name }}</span>
                <span
                  v-if="formatCoord(selectedSite.latitude) || formatCoord(selectedSite.longitude)"
                  class="text-muted-color"
                >
                  {{ formatCoord(selectedSite.latitude) || '—' }},
                  {{ formatCoord(selectedSite.longitude) || '—' }}
                </span>
              </template>
              <span v-else class="text-muted-color">No site</span>
            </div>
            <UButton
              icon="i-lucide-map-pin"
              label="Browse"
              variant="outline"
              color="neutral"
              @click="siteSelectorVisible = true"
            />
            <UButton
              v-if="selectedSite"
              label="Clear"
              variant="ghost"
              color="neutral"
              @click="clearSite"
            />
          </div>
        </UFormField>
        <UFormField label="Role">
          <UInput v-model="createForm.role" class="w-full" />
        </UFormField>
        <UFormField label="Status">
          <USelect v-model="createForm.status" :items="statusItems" class="w-full" />
        </UFormField>
      </div>
    </template>
    <template #footer="{ close }">
      <div class="flex w-full gap-2">
        <UButton label="Cancel" icon="i-lucide-x" variant="ghost" class="ms-auto" @click="close" />
        <UButton label="Create" icon="i-lucide-check" :loading="createSaving" @click="saveNew" />
      </div>
    </template>
  </FormModal>

  <SiteSelector
    v-model:visible="siteSelectorVisible"
    :selected-name="createForm.site"
    @select="onSiteSelected"
  />
</template>
