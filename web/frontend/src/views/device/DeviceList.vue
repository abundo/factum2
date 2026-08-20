<script setup>
import { useToast } from '@nuxt/ui/composables'
import { computed, onMounted, ref } from 'vue'
import {
  getDevice,
  getDeviceImpact,
  getDevices,
  refreshDeviceInterfaces,
  updateDeviceInterfaces,
} from '@/api/devices'
import {
  createXConnect,
  deleteOpticalPort,
  deleteXConnect,
  listXConnects,
  putOpticalPort,
} from '@/api/optical'
import PasswordInput from '@/components/PasswordInput.vue'
import ServiceEditDialog from '@/components/ServiceEditDialog.vue'
import SortableColumnHeader from '@/components/SortableColumnHeader.vue'
import VlanEditDialog from '@/components/VlanEditDialog.vue'
import { useDeviceCredentials } from '@/composables/useDeviceCredentials'
import { useAuthStore } from '@/stores/auth'

const toast = useToast()
const authStore = useAuthStore()
const {
  credentialsDialog,
  promptUsername,
  promptPassword,
  withCredentials,
  submitCredentials,
  cancelCredentials,
  rememberSuccess,
  rememberFailure,
} = useDeviceCredentials()

const devices = ref([])
const loading = ref(true)
const error = ref(null)

const globalFilter = ref('')
const sorting = ref([{ id: 'name', desc: false }])

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

const detailDialog = ref(false)
const interfacesDialog = ref(false)
const device = ref(null)
const deviceLoading = ref(false)
const deviceError = ref(null)

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

function addressList(iface) {
  return (iface.addresses ?? []).map((a) => a.address).join(', ')
}

// Compact VLAN summary for the interfaces table: switchport mode + up to
// maxShown VID numbers (untagged first, then tagged), with "…" when truncated.
const VLAN_SUMMARY_MAX = 3

function switchportModeLabel(mode) {
  switch ((mode ?? '').toLowerCase()) {
    case 'access':
      return 'access'
    case 'trunk':
      return 'trunk'
    case 'dot1q-tunnel':
      return 'qinq'
    default:
      return mode || ''
  }
}

function interfaceVlanIds(iface) {
  const ids = []
  if (iface.untagged_vlan) ids.push(iface.untagged_vlan)
  for (const vid of iface.tagged_vlans ?? []) {
    if (vid && vid !== iface.untagged_vlan) ids.push(vid)
  }
  return ids
}

function isSwitchport(iface) {
  const mode = (iface?.switchport_mode ?? '').toLowerCase()
  return mode === 'access' || mode === 'trunk' || mode === 'dot1q-tunnel'
}

function vlanSummary(iface) {
  // L3 ports ("no switchport" on EOS/VRP) have no traditional VLAN
  // membership - leave the cell empty even if stale untagged/tagged data
  // is still present on the record.
  if (!isSwitchport(iface)) return { text: '', title: '' }

  const mode = switchportModeLabel(iface.switchport_mode)
  const ids = interfaceVlanIds(iface)

  const fullList = ids.join(', ')
  const shown =
    ids.length > VLAN_SUMMARY_MAX ? `${ids.slice(0, VLAN_SUMMARY_MAX).join(', ')}…` : fullList

  const text = [mode, shown].filter(Boolean).join(' ')
  const titleParts = []
  if (mode) titleParts.push(`mode: ${mode}`)
  if (iface.untagged_vlan) {
    titleParts.push(
      mode === 'qinq' ? `s-vlan: ${iface.untagged_vlan}` : `untagged: ${iface.untagged_vlan}`,
    )
  }
  if ((iface.tagged_vlans ?? []).length) {
    titleParts.push(`tagged: ${(iface.tagged_vlans ?? []).join(', ')}`)
  }
  return { text, title: titleParts.join('\n') }
}

function isDescriptionChanged(iface) {
  return iface.description !== originalDescriptions.value.get(iface.id)
}

// Snapshot of each interface's description as last loaded/saved, so Update
// can tell which rows were actually edited in the datatable and only push
// those out to the device/Netbox.
const originalDescriptions = ref(new Map())

function snapshotDescriptions() {
  originalDescriptions.value = new Map(
    (device.value?.interfaces ?? []).map((iface) => [iface.id, iface.description]),
  )
}

const deviceImpact = ref(null)
const xconnects = ref([])
const xcKind = ref('tributary')
const xcA = ref(null)
const xcB = ref(null)

function addXConnect() {
  if (!device.value || !xcA.value || !xcB.value) return
  createXConnect({
    device_id: device.value.id,
    kind: xcKind.value,
    interface_a_id: xcA.value,
    interface_b_id: xcB.value,
  })
    .then(() => {
      xcA.value = null
      xcB.value = null
      loadXConnects(device.value.id)
    })
    .catch((err) => {
      toast.add({
        color: 'error',
        title: 'XConnect failed',
        description: err?.response?.data?.error,
      })
    })
}

const opticalRoles = [
  { label: '—', value: '' },
  { label: 'TXP client', value: 'txp_client' },
  { label: 'TXP line', value: 'txp_line' },
  { label: 'ROADM add/drop', value: 'roadm_adddrop' },
  { label: 'ROADM degree', value: 'roadm_degree' },
  { label: 'Fiber port', value: 'fiber_port' },
]

function savePortRole(iface, role) {
  if (!role) {
    deleteOpticalPort(iface.id).then(() => {
      iface.optical = null
    })
    return
  }
  putOpticalPort(iface.id, { role, freq_hz: iface.optical?.freq_hz || 0 }).then((p) => {
    iface.optical = p
  })
}

function loadXConnects(id) {
  if (!authStore.opticalEnabled) return
  listXConnects(id).then((data) => {
    xconnects.value = data ?? []
  })
}

function loadDevice(row) {
  device.value = null
  deviceImpact.value = null
  deviceError.value = null
  deviceLoading.value = true
  getDeviceImpact(row.id)
    .then((imp) => {
      deviceImpact.value = imp
    })
    .catch(() => {})
  loadXConnects(row.id)
  getDevice(row.id)
    .then((data) => {
      device.value = data
      snapshotDescriptions()
    })
    .catch(() => {
      deviceError.value = 'Failed to load device.'
    })
    .finally(() => {
      deviceLoading.value = false
    })
}

function showDetail(row) {
  detailDialog.value = true
  loadDevice(row)
}

function showInterfaces(row) {
  interfacesDialog.value = true
  loadDevice(row)
}

const refreshingInterfaces = ref(false)
const updatingInterfaces = ref(false)

const interfaceSorting = ref([{ id: 'name', desc: false }])
const interfaceColumns = [
  { accessorKey: 'name', header: 'Name' },
  { accessorKey: 'description', header: 'Description' },
  { id: 'vlans', header: 'VLANs' },
  { id: 'optical', header: 'Optical' },
  { id: 'services', header: 'Services' },
  { accessorKey: 'vrf', header: 'VRF' },
  { id: 'addresses', header: 'Addresses' },
]

const serviceDialogOpen = ref(false)
const editingServiceId = ref(null)

function openService(id) {
  editingServiceId.value = id
  serviceDialogOpen.value = true
}

// A service edit (e.g. changing an ELINE's endpoints) can change which
// interfaces it's linked to, so refresh the currently open device's
// interfaces to keep the Services column in sync.
function reloadDeviceInterfaces() {
  if (device.value) loadDevice(device.value)
}

// VLAN save returns the already-refreshed device (post netbox.SyncDB) - apply
// it in place so the open interfaces dialog picks up the new assignments
// without blanking the table for a second GET.
function onVlanSaved(updated) {
  if (updated?.id) {
    device.value = updated
    snapshotDescriptions()
    return
  }
  reloadDeviceInterfaces()
}

const supportedDriverPlatforms = ['eos', 'sros', 'sros-md', 'ios-xr', 'vrp']
const isSupportedDriverPlatform = computed(() =>
  supportedDriverPlatforms.includes((device.value?.platform ?? '').toLowerCase()),
)
const canUseDriver = computed(
  () =>
    authStore.canWrite &&
    isSupportedDriverPlatform.value &&
    !refreshingInterfaces.value &&
    !updatingInterfaces.value,
)

// Platforms whose driver can push switchport/VLAN config to the device
// (see globalVlanPlatforms in web/handle_device_interfaces.go) - both model
// VLANs as a device-wide VLAN database, unlike sros/sros-md/ios-xr which
// have no per-interface global-VLAN concept at all.
const globalVlanPlatforms = ['eos', 'vrp']
const isGlobalVlanPlatform = computed(() =>
  globalVlanPlatforms.includes((device.value?.platform ?? '').toLowerCase()),
)

const vlanDialogOpen = ref(false)

function openVlan() {
  vlanDialogOpen.value = true
}

function doRefreshInterfaces(username, password) {
  if (!device.value) return
  const deviceId = device.value.id
  refreshingInterfaces.value = true
  refreshDeviceInterfaces(deviceId, username, password)
    .then((data) => {
      rememberSuccess(deviceId, username, password)
      device.value = data
      snapshotDescriptions()
      toast.add({
        color: 'success',
        title: 'Interfaces refreshed',
        description: 'Descriptions were reloaded from the device.',
        duration: 3000,
      })
    })
    .catch((err) => {
      rememberFailure(deviceId, username, password)
      toast.add({
        color: 'error',
        title: 'Refresh failed',
        description: err.response?.data?.error ?? 'Failed to refresh interfaces from the device.',
        duration: 4000,
      })
    })
    .finally(() => {
      refreshingInterfaces.value = false
    })
}

function refreshInterfaces() {
  if (!device.value) return
  withCredentials(device.value.id, doRefreshInterfaces)
}

function doUpdateInterfaces(username, password) {
  if (!device.value) return
  const deviceId = device.value.id
  const interfaces = (device.value.interfaces ?? [])
    .filter((iface) => iface.description !== originalDescriptions.value.get(iface.id))
    .map((iface) => ({ id: iface.id, description: iface.description }))
  updatingInterfaces.value = true
  updateDeviceInterfaces(deviceId, username, password, interfaces)
    .then((data) => {
      rememberSuccess(deviceId, username, password)
      device.value = data
      snapshotDescriptions()
      toast.add({
        color: 'success',
        title: 'Interfaces updated',
        description: 'Descriptions were pushed to the device.',
        duration: 3000,
      })
    })
    .catch((err) => {
      rememberFailure(deviceId, username, password)
      toast.add({
        color: 'error',
        title: 'Update failed',
        description: err.response?.data?.error ?? 'Failed to update interfaces on the device.',
        duration: 4000,
      })
    })
    .finally(() => {
      updatingInterfaces.value = false
    })
}

function updateInterfaces() {
  if (!device.value) return
  const changed = (device.value.interfaces ?? []).some(
    (iface) => iface.description !== originalDescriptions.value.get(iface.id),
  )
  if (!changed) {
    toast.add({
      color: 'info',
      title: 'Nothing to update',
      description: 'No interface descriptions were changed.',
      duration: 3000,
    })
    return
  }
  withCredentials(device.value.id, doUpdateInterfaces)
}

onMounted(loadDevices)
</script>

<template>
  <div class="card">
    <div class="flex flex-wrap gap-2 items-center justify-between mb-4">
      <h4 class="m-0">Devices</h4>
      <UInput v-model="globalFilter" icon="i-lucide-search" placeholder="Search..." />
    </div>

    <UTable
      v-model:sorting="sorting"
      v-model:global-filter="globalFilter"
      :data="devices"
      :columns="columns"
      :loading="loading"
      :empty="error ?? 'No devices found.'"
      :virtualize="{ estimateSize: 46 }"
      class="max-h-[calc(100vh-320px)]"
    >
      <template
        v-for="col in columns.filter((c) => c.id !== 'actions')"
        :key="col.accessorKey"
        #[`${col.accessorKey}-header`]="{ column }"
      >
        <SortableColumnHeader :column="column" :label="col.header" />
      </template>

      <template #actions-cell="{ row }">
        <div class="flex gap-2">
          <UButton
            icon="i-lucide-pencil"
            variant="outline"
            color="neutral"
            size="sm"
            @click="showDetail(row.original)"
          />
          <UButton
            label="Interfaces"
            size="sm"
            color="neutral"
            variant="outline"
            @click="showInterfaces(row.original)"
          />
        </div>
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

  <UModal
    v-model:open="detailDialog"
    :title="device?.name ?? 'Device'"
    :ui="{ content: 'sm:max-w-2xl' }"
  >
    <template #body>
      <div v-if="deviceLoading" class="flex justify-center p-4">
        <UIcon name="i-lucide-loader-2" class="size-8 animate-spin" />
      </div>

      <UAlert v-else-if="deviceError" color="error" variant="subtle" :title="deviceError" />

      <template v-else-if="device">
        <div class="flex items-center gap-3 mb-6">
          <UBadge
            v-if="device.status"
            :label="device.status"
            :color="statusColor(device.status)"
            variant="subtle"
          />
          <UBadge v-if="!device.enabled" label="Disabled" color="neutral" variant="subtle" />
          <UBadge
            v-if="device.optical_kind"
            :label="device.optical_kind"
            color="info"
            variant="subtle"
          />
        </div>
        <div v-if="deviceImpact" class="mb-4">
          <span class="font-bold">Affected if down:</span>
          {{ deviceImpact.service_count }} services / {{ deviceImpact.customer_count }} customers
        </div>

        <div class="grid grid-cols-12 gap-4 mb-6">
          <div class="col-span-12 md:col-span-6 lg:col-span-4">
            <div class="text-sm text-muted-color mb-1">Site</div>
            <div>{{ device.site || '-' }}</div>
          </div>
          <div class="col-span-12 md:col-span-6 lg:col-span-4">
            <div class="text-sm text-muted-color mb-1">Role</div>
            <div>{{ device.role || '-' }}</div>
          </div>
          <div class="col-span-12 md:col-span-6 lg:col-span-4">
            <div class="text-sm text-muted-color mb-1">Manufacturer</div>
            <div>{{ device.manufacturer || '-' }}</div>
          </div>
          <div class="col-span-12 md:col-span-6 lg:col-span-4">
            <div class="text-sm text-muted-color mb-1">Model</div>
            <div>{{ device.model_name || '-' }}</div>
          </div>
          <div class="col-span-12 md:col-span-6 lg:col-span-4">
            <div class="text-sm text-muted-color mb-1">Platform</div>
            <div>{{ device.platform || '-' }}</div>
          </div>
          <div class="col-span-12 md:col-span-6 lg:col-span-4">
            <div class="text-sm text-muted-color mb-1">Primary IPv4</div>
            <div>{{ device.primary_ipv4 || '-' }}</div>
          </div>
          <div class="col-span-12 md:col-span-6 lg:col-span-4">
            <div class="text-sm text-muted-color mb-1">Primary IPv6</div>
            <div>{{ device.primary_ipv6 || '-' }}</div>
          </div>
          <div class="col-span-12 md:col-span-6 lg:col-span-4">
            <div class="text-sm text-muted-color mb-1">Location</div>
            <div>{{ device.cf_location || '-' }}</div>
          </div>
          <div class="col-span-12">
            <div class="text-sm text-muted-color mb-1">Comments</div>
            <div>{{ device.comments || '-' }}</div>
          </div>
        </div>

        <div class="flex flex-wrap gap-2">
          <UBadge v-if="device.cf_monitor_icinga" label="Icinga" color="info" variant="subtle" />
          <UBadge
            v-if="device.cf_monitor_librenms"
            label="LibreNMS"
            color="info"
            variant="subtle"
          />
          <UBadge v-if="device.cf_monitor_grafana" label="Grafana" color="info" variant="subtle" />
          <UBadge
            v-if="device.cf_backup_oxidized"
            label="Oxidized backup"
            color="info"
            variant="subtle"
          />
          <UBadge
            v-if="device.cf_alarm_interfaces"
            label="Interface alarms"
            color="info"
            variant="subtle"
          />
        </div>
      </template>
    </template>

    <template #footer>
      <UButton label="Close" icon="i-lucide-x" variant="ghost" @click="detailDialog = false" />
    </template>
  </UModal>

  <UModal
    v-model:open="interfacesDialog"
    :title="device?.name ? `${device.name} - Interfaces` : 'Interfaces'"
    :ui="{ content: 'w-[95vw] h-[90vh] sm:max-w-none' }"
  >
    <template #body>
      <div v-if="deviceLoading" class="flex justify-center p-4">
        <UIcon name="i-lucide-loader-2" class="size-8 animate-spin" />
      </div>

      <UAlert v-else-if="deviceError" color="error" variant="subtle" :title="deviceError" />

      <div v-else class="flex flex-col h-full min-h-0">
        <div class="flex flex-wrap items-end gap-2 mb-4 shrink-0">
          <UButton
            label="Refresh"
            icon="i-lucide-refresh-cw"
            size="sm"
            variant="outline"
            color="neutral"
            :loading="refreshingInterfaces"
            :disabled="!canUseDriver"
            @click="refreshInterfaces"
          />
          <UButton
            label="Save changes"
            icon="i-lucide-save"
            size="sm"
            :loading="updatingInterfaces"
            :disabled="!canUseDriver"
            @click="updateInterfaces"
          />
          <UButton
            v-if="isGlobalVlanPlatform"
            label="VLAN"
            icon="i-lucide-network"
            size="sm"
            variant="outline"
            color="neutral"
            :disabled="!authStore.canWrite || deviceLoading"
            @click="openVlan"
          />
          <span v-if="!isSupportedDriverPlatform" class="text-sm text-muted-color"
            >Refresh/Update require an EOS, SROS-MD, IOS-XR or VRP device (this device is "{{
              device?.platform || 'unknown'
            }}").</span
          >
        </div>

        <UTable
          v-model:sorting="interfaceSorting"
          :data="device?.interfaces ?? []"
          :columns="interfaceColumns"
          :empty="'No interfaces stored for this device.'"
          sticky
          class="flex-1 min-h-0 overflow-y-auto"
        >
          <template #name-header="{ column }">
            <SortableColumnHeader :column="column" label="Name" />
          </template>
          <template #description-header="{ column }">
            <SortableColumnHeader :column="column" label="Description" />
          </template>
          <template #vrf-header="{ column }">
            <SortableColumnHeader :column="column" label="VRF" />
          </template>

          <template #name-cell="{ row }">
            <span class="whitespace-nowrap">{{ row.original.name }}</span>
          </template>
          <template #description-cell="{ row }">
            <div class="flex items-center gap-1">
              <UInput
                v-model="row.original.description"
                :disabled="!authStore.canWrite"
                size="sm"
                class="w-full min-w-lg"
              />
              <span
                v-if="isDescriptionChanged(row.original)"
                title="Changed, not yet saved"
                class="size-1.5 rounded-full bg-warning shrink-0"
              />
            </div>
          </template>
          <template #vlans-cell="{ row }">
            <span
              class="whitespace-nowrap text-sm"
              :title="vlanSummary(row.original).title || undefined"
              >{{ vlanSummary(row.original).text || '—' }}</span
            >
          </template>
          <template #optical-cell="{ row }">
            <div class="flex items-center gap-1">
              <USelect
                v-if="authStore.opticalEnabled && authStore.canWrite"
                :model-value="row.original.optical?.role || ''"
                :items="opticalRoles"
                value-key="value"
                label-key="label"
                class="w-36"
                @update:model-value="savePortRole(row.original, $event)"
              />
              <span v-else>{{ row.original.optical?.role || '—' }}</span>
              <UInput
                v-if="
                  row.original.optical?.role === 'roadm_adddrop' ||
                  row.original.optical?.role === 'txp_line'
                "
                class="w-24"
                placeholder="THz"
                :model-value="
                  row.original.optical?.freq_hz
                    ? (row.original.optical.freq_hz / 1e12).toFixed(4)
                    : ''
                "
                @change="
                  (e) =>
                    putOpticalPort(row.original.id, {
                      role: row.original.optical.role,
                      freq_thz: Number(e.target.value),
                    }).then((p) => {
                      row.original.optical = p
                    })
                "
              />
            </div>
          </template>
          <template #services-cell="{ row }">
            <div class="flex flex-wrap gap-1">
              <UButton
                v-for="svc in row.original.services ?? []"
                :key="svc.id"
                :label="svc.service_id || 'Service'"
                icon="i-lucide-link"
                size="sm"
                variant="outline"
                color="neutral"
                @click="openService(svc.id)"
              />
            </div>
          </template>
          <template #addresses-cell="{ row }">
            <span class="whitespace-nowrap">{{ addressList(row.original) }}</span>
          </template>
        </UTable>
        <div v-if="authStore.opticalEnabled" class="mt-4">
          <div class="font-bold mb-2">Cross-connects</div>
          <ul>
            <li v-for="x in xconnects" :key="x.id">
              {{ x.kind }} · {{ x.interface_a_id }} ↔ {{ x.interface_b_id }}
              <UButton
                v-if="authStore.canWrite"
                icon="i-lucide-trash"
                size="xs"
                variant="ghost"
                color="error"
                @click="deleteXConnect(x.id).then(() => loadXConnects(device.id))"
              />
            </li>
          </ul>
          <div v-if="authStore.canWrite" class="flex flex-wrap gap-2 mt-2">
            <USelect
              v-model="xcKind"
              :items="[
                { label: 'Tributary', value: 'tributary' },
                { label: 'Add/drop', value: 'roadm_adddrop' },
                { label: 'Express', value: 'roadm_express' },
                { label: 'Passthrough', value: 'passthrough' },
              ]"
              value-key="value"
              label-key="label"
            />
            <USelect
              v-model="xcA"
              :items="(device.interfaces ?? []).map((i) => ({ label: i.name, value: i.id }))"
              value-key="value"
              label-key="label"
              placeholder="A"
            />
            <USelect
              v-model="xcB"
              :items="(device.interfaces ?? []).map((i) => ({ label: i.name, value: i.id }))"
              value-key="value"
              label-key="label"
              placeholder="B"
            />
            <UButton label="Add" @click="addXConnect" />
          </div>
        </div>
      </div>
    </template>

    <template #footer>
      <UButton label="Close" icon="i-lucide-x" variant="ghost" @click="interfacesDialog = false" />
    </template>
  </UModal>

  <UModal
    v-model:open="credentialsDialog"
    title="Device credentials"
    :ui="{ content: 'sm:max-w-sm' }"
    @update:open="(open) => !open && cancelCredentials()"
  >
    <template #body>
      <div class="flex flex-col gap-3">
        <div class="flex flex-col gap-1">
          <label for="prompt-username" class="text-sm text-muted-color">Username</label>
          <UInput
            id="prompt-username"
            v-model="promptUsername"
            autocomplete="off"
            autofocus
            class="w-full"
            @keyup.enter="submitCredentials"
          />
        </div>
        <div class="flex flex-col gap-1">
          <label for="prompt-password" class="text-sm text-muted-color">Password</label>
          <PasswordInput
            id="prompt-password"
            v-model="promptPassword"
            autocomplete="new-password"
            @keyup.enter="submitCredentials"
          />
        </div>
      </div>
    </template>

    <template #footer>
      <UButton label="Cancel" icon="i-lucide-x" variant="ghost" @click="cancelCredentials" />
      <UButton
        label="Continue"
        icon="i-lucide-check"
        :disabled="!promptUsername || !promptPassword"
        @click="submitCredentials"
      />
    </template>
  </UModal>

  <ServiceEditDialog
    v-model:open="serviceDialogOpen"
    :service-id="editingServiceId"
    @saved="reloadDeviceInterfaces"
    @deleted="reloadDeviceInterfaces"
  />

  <VlanEditDialog
    v-model:open="vlanDialogOpen"
    :interfaces="device?.interfaces ?? []"
    :device-id="device?.id"
    :device-name="device?.name"
    :platform="device?.platform"
    @saved="onVlanSaved"
  />
</template>
