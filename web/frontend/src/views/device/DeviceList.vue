<script setup>
import { useToast } from '@nuxt/ui/composables'
import { computed, onMounted, ref, watch } from 'vue'
import {
  createDevice,
  deleteDevice,
  getDevice,
  getDeviceImpact,
  getDevices,
  refreshDeviceInterfaces,
  updateDevice,
  updateDeviceInterfaces,
} from '@/api/devices'
import {
  createAddress,
  createInterface,
  deleteAddress,
  deleteInterface,
  getDeviceTypes,
  getManufacturers,
  getPlatforms,
  updateAddress,
  updateInterface,
} from '@/api/dcim'
import { getSite, getSites } from '@/api/sites'
import { interfaceTypeItems } from '@/utils/interfaceTypes'
import {
  createXConnect,
  deleteOpticalPort,
  deleteXConnect,
  listXConnects,
  putOpticalPort,
} from '@/api/optical'
import OxidizedNodePanel from '@/components/OxidizedNodePanel.vue'
import AttachServiceDialog from '@/components/AttachServiceDialog.vue'
import IpamAddressPicker from '@/components/IpamAddressPicker.vue'
import SearchInput from '@/components/SearchInput.vue'
import SiteSelector from '@/components/SiteSelector.vue'
import ServiceEditDialog from '@/components/ServiceEditDialog.vue'
import SortableColumnHeader from '@/components/SortableColumnHeader.vue'
import VlanEditDialog from '@/components/VlanEditDialog.vue'
import { useAuthStore } from '@/stores/auth'

const toast = useToast()
const authStore = useAuthStore()

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

const createDialog = ref(false)
const createSaving = ref(false)
const siteSelectorVisible = ref(false)
const selectedSite = ref(null)
const sites = ref([])

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
const deletingDevice = ref(false)

const isLocalDevice = computed(
  () => device.value && !device.value.netbox_id && device.value.cf_source !== 'netbox',
)

const manufacturerById = computed(() => {
  const map = new Map()
  for (const m of manufacturers.value) map.set(m.id, m.name)
  return map
})
const deviceTypeItems = computed(() =>
  deviceTypes.value.map((dt) => ({
    label: `${manufacturerById.value.get(dt.manufacturer_id) || '?'} ${dt.model}`,
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
const opticalKindItems = [
  { label: 'None', value: 'none' },
  { label: 'ROADM', value: 'roadm' },
  { label: 'WDM shelf', value: 'wdm_shelf' },
  { label: 'ILA / amplifier', value: 'ila' },
  { label: 'Passive / ODF', value: 'passive' },
]

const detailDialog = ref(false)
const detailTab = ref('overview')
const vlanEditor = ref(null)
const device = ref(null)
const deviceLoading = ref(false)
const deviceError = ref(null)

const detailTabItems = computed(() => {
  const items = [
    { label: 'Overview', value: 'overview', slot: 'overview' },
    { label: 'Interfaces', value: 'interfaces', slot: 'interfaces' },
    { label: 'VLANs', value: 'vlans', slot: 'vlans' },
  ]
  if (authStore.oxidizedEnabled) {
    items.push({ label: 'Oxidized', value: 'oxidized', slot: 'oxidized' })
  }
  return items
})

const deviceTypeLabel = computed(() => {
  if (!device.value) return ''
  if (isLocalDevice.value) {
    return (
      deviceTypeItems.value.find((i) => i.value === createForm.value.device_type_id)?.label || ''
    )
  }
  return [device.value.manufacturer, device.value.model_name].filter(Boolean).join(' ')
})

const deviceLocation = computed(() => {
  if (!device.value) return ''
  return (isLocalDevice.value ? createForm.value.cf_location : device.value.cf_location) || ''
})

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

function resolveSelectedSite(d) {
  const id = createForm.value.site_id || d?.site_id || 0
  const name = (createForm.value.site || d?.site || '').trim()
  const found =
    (id && sites.value.find((s) => s.id === id)) ||
    (name && sites.value.find((s) => (s.name || '').toLowerCase() === name.toLowerCase())) ||
    null
  selectedSite.value = found
  if (found) {
    createForm.value.site_id = found.id
    createForm.value.site = found.name
  }
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
    if (dt) createForm.value.platform_id = dt.platform_id || 0
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

function fillFormFromDevice(d) {
  const mfr = manufacturers.value.find((m) => m.name === d.manufacturer)
  const dt = deviceTypes.value.find(
    (t) => t.model === d.model_name && (!mfr || t.manufacturer_id === mfr.id),
  )
  const plat = platforms.value.find((p) => p.slug === d.platform || p.name === d.platform)
  createForm.value = {
    name: d.name ?? '',
    device_type_id: d.device_type_id || dt?.id,
    platform_id: plat?.id || 0,
    site: d.site ?? '',
    site_id: d.site_id || 0,
    role: d.role ?? '',
    status: d.status || 'active',
    comments: d.comments ?? '',
    enabled: d.enabled !== false,
    cf_location: d.cf_location ?? '',
    cf_monitor_icinga: !!d.cf_monitor_icinga,
    cf_monitor_librenms: !!d.cf_monitor_librenms,
    cf_monitor_grafana: !!d.cf_monitor_grafana,
    cf_backup_oxidized: !!d.cf_backup_oxidized,
    cf_alarm_interfaces: !!d.cf_alarm_interfaces,
    optical_kind: d.optical_kind || 'none',
  }
}

function saveLocalDevice() {
  if (!device.value) return
  if (!createForm.value.name.trim()) {
    toast.add({ color: 'error', title: 'Name is required' })
    return
  }
  if (!createForm.value.device_type_id) {
    toast.add({ color: 'error', title: 'Device type is required' })
    return
  }
  createSaving.value = true
  updateDevice(device.value.id, deviceWritePayload(createForm.value))
    .then((data) => {
      device.value = data
      snapshotDescriptions()
      loadDevices()
      toast.add({ color: 'success', title: 'Device saved', duration: 3000 })
    })
    .catch((err) => {
      toast.add({
        color: 'error',
        title: 'Save failed',
        description: err?.response?.data?.error,
      })
    })
    .finally(() => {
      createSaving.value = false
    })
}

function removeLocalDevice() {
  if (!device.value) return
  deletingDevice.value = true
  deleteDevice(device.value.id)
    .then(() => {
      detailDialog.value = false
      loadDevices()
    })
    .catch((err) => {
      toast.add({
        color: 'error',
        title: 'Delete failed',
        description: err?.response?.data?.error,
      })
    })
    .finally(() => {
      deletingDevice.value = false
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
  // L3 ports ("no switchport" on EOS/VRP/Cisco SMB) have no traditional VLAN
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

const interfacesDirty = computed(() =>
  (device.value?.interfaces ?? []).some((iface) => isDescriptionChanged(iface)),
)

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

// Intra-device optical xconnects (tributary, add/drop, express, passthrough)
// only apply to classified optical chassis — not packet routers/switches.
const isOpticalDevice = computed(() => {
  const kind = (device.value?.optical_kind ?? '').toLowerCase()
  return ['roadm', 'wdm_shelf', 'ila', 'passive'].includes(kind)
})

const xcKindItems = computed(() => {
  switch ((device.value?.optical_kind ?? '').toLowerCase()) {
    case 'roadm':
      // Combo ROADMs may also host TXP cards, so tributary stays available.
      return [
        { label: 'Tributary', value: 'tributary' },
        { label: 'Add/drop', value: 'roadm_adddrop' },
        { label: 'Express', value: 'roadm_express' },
      ]
    case 'wdm_shelf':
      return [{ label: 'Tributary', value: 'tributary' }]
    case 'ila':
    case 'passive':
      return [{ label: 'Passthrough', value: 'passthrough' }]
    default:
      return []
  }
})

const xcKindLabels = {
  tributary: 'Tributary',
  roadm_adddrop: 'Add/drop',
  roadm_express: 'Express',
  passthrough: 'Passthrough',
}

function defaultXcKind(opticalKind) {
  switch ((opticalKind ?? '').toLowerCase()) {
    case 'roadm':
      return 'roadm_adddrop'
    case 'ila':
    case 'passive':
      return 'passthrough'
    default:
      return 'tributary'
  }
}

function interfaceNameById(id) {
  return (device.value?.interfaces ?? []).find((i) => i.id === id)?.name ?? `#${id}`
}

const xcInterfaceItems = computed(() =>
  (device.value?.interfaces ?? []).map((i) => ({ label: i.name, value: i.id })),
)

watch(xcKind, () => {
  xcA.value = null
  xcB.value = null
})

function resetXcForm(opticalKind) {
  xcKind.value = defaultXcKind(opticalKind)
  xcA.value = null
  xcB.value = null
}

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
  const id = row?.id
  if (!id) return
  const same = device.value?.id === id
  if (!same) {
    device.value = null
    deviceImpact.value = null
    xconnects.value = []
  }
  deviceError.value = null
  deviceLoading.value = !same
  getDeviceImpact(id)
    .then((imp) => {
      deviceImpact.value = imp
    })
    .catch(() => {})
  loadXConnects(id)
  getDevice(id)
    .then((data) => {
      device.value = data
      snapshotDescriptions()
      resetXcForm(data.optical_kind)
      const local = !data.netbox_id && data.cf_source !== 'netbox'
      Promise.all([getManufacturers(), getDeviceTypes(), getPlatforms(), getSites()])
        .then(([mfrs, types, plats, siteRows]) => {
          manufacturers.value = mfrs ?? []
          deviceTypes.value = types ?? []
          platforms.value = plats ?? []
          sites.value = siteRows ?? []
          if (local) fillFormFromDevice(data)
          else {
            createForm.value.site = data.site ?? ''
            createForm.value.site_id = data.site_id || 0
          }
          resolveSelectedSite(data)
        })
        .catch(() => {
          if (local) fillFormFromDevice(data)
          resolveSelectedSite(data)
        })
    })
    .catch(() => {
      if (!same) deviceError.value = 'Failed to load device.'
    })
    .finally(() => {
      deviceLoading.value = false
    })
}

function showDetail(row) {
  detailTab.value = 'overview'
  detailDialog.value = true
  loadDevice(row)
}

const refreshingInterfaces = ref(false)
const updatingInterfaces = ref(false)

const ifaceFormOpen = ref(false)
const ifaceSaving = ref(false)
const ifaceDeleting = ref(false)
const ifaceEditingId = ref(null)
const ifaceForm = ref(emptyIfaceForm())
const ifaceDialogTitle = computed(() => (ifaceEditingId.value ? 'Edit interface' : 'New interface'))
const ifaceFormWritable = computed(() => {
  if (!ifaceEditingId.value) return isLocalDevice.value
  if (!isLocalDevice.value) return false
  const iface = (device.value?.interfaces ?? []).find((i) => i.id === ifaceEditingId.value)
  return !iface?.netbox_id
})

function emptyIfaceForm() {
  return { name: '', type: '1000base-t', label: '', description: '', vrf: '', enabled: true }
}

function openNewIface() {
  ifaceEditingId.value = null
  ifaceForm.value = emptyIfaceForm()
  ifaceFormOpen.value = true
}

function openEditIface(iface) {
  ifaceEditingId.value = iface.id
  ifaceForm.value = {
    name: iface.name ?? '',
    type: iface.type || 'other',
    label: iface.label ?? '',
    description: iface.description ?? '',
    vrf: iface.vrf ?? '',
    enabled: !!iface.enabled,
  }
  ifaceFormOpen.value = true
}

function saveIface() {
  if (!device.value) return
  if (!ifaceFormWritable.value) return
  if (!ifaceForm.value.name.trim()) {
    toast.add({ color: 'error', title: 'Name is required' })
    return
  }
  ifaceSaving.value = true
  const payload = {
    device_id: device.value.id,
    name: ifaceForm.value.name.trim(),
    type: ifaceForm.value.type,
    label: ifaceForm.value.label.trim(),
    description: ifaceForm.value.description.trim(),
    vrf: ifaceForm.value.vrf.trim(),
    enabled: ifaceForm.value.enabled,
  }
  const req = ifaceEditingId.value
    ? updateInterface(ifaceEditingId.value, payload)
    : createInterface(payload)
  req
    .then(() => {
      ifaceFormOpen.value = false
      reloadDeviceInterfaces()
    })
    .catch((err) => {
      toast.add({
        color: 'error',
        title: ifaceEditingId.value ? 'Update failed' : 'Create failed',
        description: err?.response?.data?.error,
      })
    })
    .finally(() => {
      ifaceSaving.value = false
    })
}

const addrFormOpen = ref(false)
const addrPickerOpen = ref(false)
const addrPickerVrf = ref('')
const addrSaving = ref(false)
const addrDeleting = ref(false)
const addrEditingId = ref(null)
const addrForm = ref({
  interface_id: 0,
  interface_name: '',
  address: '',
  dns_name: '',
  vrf: '',
  role: '',
  management: false,
})
const addrDialogTitle = computed(() =>
  addrEditingId.value
    ? `Edit IP address on ${addrForm.value.interface_name || 'interface'}`
    : `Add IP address on ${addrForm.value.interface_name || 'interface'}`,
)

function isManagementAddr(addr) {
  const d = device.value
  if (!d || !addr) return false
  const ids = [d.primary_ipv4_id, d.primary_ipv6_id]
  if (addr.id && ids.includes(addr.id)) return true
  if (addr.netbox_id && ids.includes(addr.netbox_id)) return true
  const a = addr.address || ''
  return !!a && (a === d.primary_ipv4 || a === d.primary_ipv6)
}

function openAddAddr(iface) {
  addrEditingId.value = null
  addrPickerVrf.value = iface.vrf ?? ''
  addrForm.value = {
    interface_id: iface.id,
    interface_name: iface.name,
    address: '',
    dns_name: '',
    vrf: iface.vrf ?? '',
    role: '',
    management: false,
  }
  addrFormOpen.value = true
  if (authStore.ipamEnabled) addrPickerOpen.value = true
}

function openEditAddr(iface, addr) {
  addrEditingId.value = addr.id
  addrPickerVrf.value = iface.vrf ?? ''
  addrForm.value = {
    interface_id: iface.id,
    interface_name: iface.name,
    address: addr.address ?? '',
    dns_name: addr.dns_name ?? '',
    vrf: addr.vrf ?? '',
    role: addr.role ?? '',
    management: isManagementAddr(addr),
  }
  addrFormOpen.value = true
}

function onPickAddr(sel) {
  addrForm.value.address = sel.address ?? ''
  if (sel.vrf != null) addrForm.value.vrf = sel.vrf
}

function saveNewAddr() {
  if (!addrForm.value.address.trim()) {
    toast.add({ color: 'error', title: 'Address is required' })
    return
  }
  addrSaving.value = true
  const payload = {
    interface_id: addrForm.value.interface_id,
    address: addrForm.value.address.trim(),
    dns_name: addrForm.value.dns_name.trim(),
    vrf: addrForm.value.vrf.trim(),
    role: addrForm.value.role.trim(),
    management: !!addrForm.value.management,
  }
  const req = addrEditingId.value
    ? updateAddress(addrEditingId.value, payload)
    : createAddress(payload)
  req
    .then(() => {
      addrFormOpen.value = false
      reloadDeviceInterfaces()
      loadDevices()
    })
    .catch((err) => {
      toast.add({
        color: 'error',
        title: addrEditingId.value ? 'Update address failed' : 'Add address failed',
        description: err?.response?.data?.error,
      })
    })
    .finally(() => {
      addrSaving.value = false
    })
}

function removeAddr(addr) {
  addrDeleting.value = true
  deleteAddress(addr.id)
    .then(() => {
      reloadDeviceInterfaces()
      loadDevices()
    })
    .catch((err) => {
      toast.add({
        color: 'error',
        title: 'Delete failed',
        description: err?.response?.data?.error,
      })
    })
    .finally(() => {
      addrDeleting.value = false
    })
}

function removeIface(row) {
  ifaceDeleting.value = true
  deleteInterface(row.id)
    .then(() => reloadDeviceInterfaces())
    .catch((err) => {
      toast.add({
        color: 'error',
        title: 'Delete failed',
        description: err?.response?.data?.error,
      })
    })
    .finally(() => {
      ifaceDeleting.value = false
    })
}

const interfaceSorting = ref([{ id: 'name', desc: false }])
const interfaceColumns = computed(() => {
  const cols = [{ id: 'actions', header: '' }]
  cols.push(
    { accessorKey: 'name', header: 'Name' },
    { accessorKey: 'type', header: 'Type' },
    { accessorKey: 'description', header: 'Description' },
    { id: 'vlans', header: 'VLANs' },
  )
  if (authStore.opticalEnabled && isOpticalDevice.value) {
    cols.push({ id: 'optical', header: 'Optical' })
  }
  cols.push(
    { id: 'services', header: 'Services' },
    { accessorKey: 'vrf', header: 'VRF' },
    { id: 'addresses', header: 'Addresses' },
  )
  return cols
})

const serviceDialogOpen = ref(false)
const editingServiceId = ref(null)
const attachOpen = ref(false)
const attachTarget = ref(null)

function openService(id) {
  editingServiceId.value = id
  serviceDialogOpen.value = true
}

function openAttach(iface) {
  attachTarget.value = {
    deviceId: device.value?.id,
    deviceName: device.value?.name,
    interfaceId: iface.id,
    interfaceName: iface.name,
  }
  attachOpen.value = true
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

const supportedDriverPlatforms = ['eos', 'sros', 'sros-md', 'ios-xr', 'vrp', 'ciscosmb']
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
// (see globalVlanPlatforms in web/handle_device_interfaces.go) - they model
// VLANs as a device-wide VLAN database, unlike sros/sros-md/ios-xr which
// have no per-interface global-VLAN concept at all.
const globalVlanPlatforms = ['eos', 'vrp', 'ciscosmb']
const isGlobalVlanPlatform = computed(() =>
  globalVlanPlatforms.includes((device.value?.platform ?? '').toLowerCase()),
)

const vlanDirty = computed(() => !!vlanEditor.value?.hasChanges)
const detailDirty = computed(() => interfacesDirty.value || vlanDirty.value)

function refreshInterfaces() {
  if (!device.value) return
  refreshingInterfaces.value = true
  refreshDeviceInterfaces(device.value.id)
    .then((data) => {
      device.value = data
      snapshotDescriptions()
      toast.add({
        color: 'success',
        title: 'Interfaces refreshed',
        description: 'Interfaces were reloaded from the device.',
        duration: 3000,
      })
    })
    .catch((err) => {
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

function updateInterfaces() {
  if (!device.value) return
  const interfaces = (device.value.interfaces ?? [])
    .filter((iface) => iface.description !== originalDescriptions.value.get(iface.id))
    .map((iface) => ({ id: iface.id, description: iface.description }))
  if (!interfaces.length) {
    toast.add({
      color: 'info',
      title: 'Nothing to update',
      description: 'No interface descriptions were changed.',
      duration: 3000,
    })
    return
  }
  updatingInterfaces.value = true
  updateDeviceInterfaces(device.value.id, interfaces)
    .then((data) => {
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

onMounted(loadDevices)
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
      <SearchInput v-model="globalFilter" />
    </div>

    <UTable
      v-model:sorting="sorting"
      v-model:global-filter="globalFilter"
      :data="devices"
      :columns="columns"
      :loading="loading"
      :empty="error ?? 'No devices found.'"
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
          color="neutral"
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

  <FormModal
    v-model:open="detailDialog"
    :dirty="detailDirty"
    :title="device?.name ?? 'Device'"
    :ui="{
      content: 'w-[95vw] h-[90vh] sm:max-w-none flex flex-col',
      header: 'min-w-0',
      title: 'min-w-0 flex-1',
      body: 'flex-1 min-h-0 overflow-hidden',
    }"
  >
    <template #title>
      <div class="flex min-w-0 flex-wrap items-baseline gap-x-4 gap-y-0.5 pr-2">
        <span class="truncate">{{ device?.name ?? 'Device' }}</span>
        <span
          v-if="deviceTypeLabel"
          class="text-sm font-normal text-muted-color truncate"
          :title="deviceTypeLabel"
          >{{ deviceTypeLabel }}</span
        >
        <span
          v-if="deviceLocation"
          class="text-sm font-normal text-muted-color truncate"
          :title="deviceLocation"
          >{{ deviceLocation }}</span
        >
      </div>
    </template>
    <template #body>
      <div v-if="deviceLoading" class="flex justify-center p-4">
        <UIcon name="i-lucide-loader-2" class="size-8 animate-spin" />
      </div>

      <UAlert v-else-if="deviceError" color="error" variant="subtle" :title="deviceError" />

      <div v-else-if="device" class="flex flex-col h-full min-h-0">
        <UTabs
          v-model="detailTab"
          :items="detailTabItems"
          class="min-h-0 flex-1"
          :ui="{
            list: 'w-full',
            content: 'min-h-0 flex-1 overflow-auto rounded-md border border-default p-3 mt-2',
          }"
        >
          <template #overview>
            <div class="grid grid-cols-[9rem_minmax(0,1fr)] items-center gap-y-3 gap-x-3">
              <span class="font-bold whitespace-nowrap">Status</span>
              <div class="flex flex-wrap items-center gap-2">
                <UBadge
                  v-if="isLocalDevice ? createForm.status : device.status"
                  :label="isLocalDevice ? createForm.status : device.status"
                  :color="statusColor(isLocalDevice ? createForm.status : device.status)"
                  variant="subtle"
                />
                <UBadge
                  v-if="!(isLocalDevice ? createForm.enabled : device.enabled)"
                  label="Disabled"
                  color="neutral"
                  variant="subtle"
                />
                <UBadge
                  v-if="!isLocalDevice && device.optical_kind"
                  :label="device.optical_kind"
                  color="info"
                  variant="subtle"
                />
                <span
                  v-if="!isLocalDevice && !device.status && device.enabled && !device.optical_kind"
                  class="text-muted-color"
                  >—</span
                >
              </div>

              <template v-if="isLocalDevice">
                <label for="device-enabled" class="font-bold whitespace-nowrap">Enabled</label>
                <USwitch id="device-enabled" v-model="createForm.enabled" />
              </template>

              <template v-if="deviceImpact">
                <span class="font-bold whitespace-nowrap">Affected</span>
                <span class="min-w-0">{{
                  `${deviceImpact.service_count} services / ${deviceImpact.customer_count} customers`
                }}</span>
              </template>

              <label for="device-name" class="font-bold whitespace-nowrap">Name</label>
              <UInput
                v-if="isLocalDevice"
                id="device-name"
                v-model="createForm.name"
                class="w-full"
              />
              <UInput
                v-else
                id="device-name"
                :model-value="device.name || ''"
                disabled
                class="w-full"
              />

              <label class="font-bold whitespace-nowrap">Site</label>
              <div class="min-w-0 flex items-center gap-2">
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
                  <span v-else class="text-muted-color">{{ device.site || 'No site' }}</span>
                </div>
                <template v-if="isLocalDevice && authStore.canWrite">
                  <UButton
                    icon="i-lucide-map-pin"
                    label="Browse"
                    variant="outline"
                    color="neutral"
                    @click="siteSelectorVisible = true"
                  />
                  <UButton
                    v-if="selectedSite || createForm.site"
                    label="Clear"
                    variant="ghost"
                    color="neutral"
                    @click="clearSite"
                  />
                </template>
              </div>

              <label for="device-role" class="font-bold whitespace-nowrap">Role</label>
              <UInput
                v-if="isLocalDevice"
                id="device-role"
                v-model="createForm.role"
                class="w-full"
              />
              <UInput
                v-else
                id="device-role"
                :model-value="device.role || ''"
                disabled
                class="w-full"
              />

              <label for="device-type" class="font-bold whitespace-nowrap">Device type</label>
              <USelect
                v-if="isLocalDevice"
                id="device-type"
                v-model="createForm.device_type_id"
                :items="deviceTypeItems"
                class="w-full"
              />
              <UInput
                v-else
                id="device-type"
                :model-value="
                  [device.manufacturer, device.model_name].filter(Boolean).join(' ') || ''
                "
                disabled
                class="w-full"
              />

              <label for="device-platform" class="font-bold whitespace-nowrap">Platform</label>
              <USelect
                v-if="isLocalDevice"
                id="device-platform"
                v-model="createForm.platform_id"
                :items="platformItems"
                class="w-full"
              />
              <UInput
                v-else
                id="device-platform"
                :model-value="device.platform || ''"
                disabled
                class="w-full"
              />

              <template v-if="isLocalDevice">
                <label for="device-status" class="font-bold whitespace-nowrap">Status</label>
                <USelect
                  id="device-status"
                  v-model="createForm.status"
                  :items="statusItems"
                  class="w-full"
                />
              </template>

              <label for="device-ipv4" class="font-bold whitespace-nowrap">Primary IPv4</label>
              <UInput
                id="device-ipv4"
                :model-value="device.primary_ipv4 || ''"
                disabled
                class="w-full font-mono"
                title="Set from a management IP address on an interface"
              />

              <label for="device-ipv6" class="font-bold whitespace-nowrap">Primary IPv6</label>
              <UInput
                id="device-ipv6"
                :model-value="device.primary_ipv6 || ''"
                disabled
                class="w-full font-mono"
                title="Set from a management IP address on an interface"
              />

              <label for="device-location" class="font-bold whitespace-nowrap">Location</label>
              <UInput
                v-if="isLocalDevice"
                id="device-location"
                v-model="createForm.cf_location"
                class="w-full"
              />
              <UInput
                v-else
                id="device-location"
                :model-value="device.cf_location || ''"
                disabled
                class="w-full"
              />

              <template v-if="authStore.opticalEnabled">
                <label for="device-optical-kind" class="font-bold whitespace-nowrap"
                  >Optical kind</label
                >
                <USelect
                  v-if="isLocalDevice"
                  id="device-optical-kind"
                  v-model="createForm.optical_kind"
                  :items="opticalKindItems"
                  class="w-full"
                />
                <UInput
                  v-else
                  id="device-optical-kind"
                  :model-value="device.optical_kind || ''"
                  disabled
                  class="w-full"
                />
              </template>

              <label for="device-comments" class="font-bold whitespace-nowrap self-start mt-2"
                >Comments</label
              >
              <UTextarea
                v-if="isLocalDevice"
                id="device-comments"
                v-model="createForm.comments"
                :rows="2"
                class="w-full"
              />
              <UTextarea
                v-else
                id="device-comments"
                :model-value="device.comments || ''"
                disabled
                :rows="2"
                class="w-full"
              />

              <span class="font-bold whitespace-nowrap self-start mt-1">Monitoring</span>
              <div class="flex flex-col gap-2">
                <label class="flex items-center gap-2">
                  <USwitch v-if="isLocalDevice" v-model="createForm.cf_monitor_icinga" />
                  <USwitch v-else :model-value="!!device.cf_monitor_icinga" disabled />
                  <span>Icinga</span>
                </label>
                <label class="flex items-center gap-2">
                  <USwitch v-if="isLocalDevice" v-model="createForm.cf_monitor_librenms" />
                  <USwitch v-else :model-value="!!device.cf_monitor_librenms" disabled />
                  <span>LibreNMS</span>
                </label>
                <label class="flex items-center gap-2">
                  <USwitch v-if="isLocalDevice" v-model="createForm.cf_monitor_grafana" />
                  <USwitch v-else :model-value="!!device.cf_monitor_grafana" disabled />
                  <span>Grafana</span>
                </label>
                <label class="flex items-center gap-2">
                  <USwitch v-if="isLocalDevice" v-model="createForm.cf_backup_oxidized" />
                  <USwitch v-else :model-value="!!device.cf_backup_oxidized" disabled />
                  <span>Oxidized backup</span>
                </label>
                <label class="flex items-center gap-2">
                  <USwitch v-if="isLocalDevice" v-model="createForm.cf_alarm_interfaces" />
                  <USwitch v-else :model-value="!!device.cf_alarm_interfaces" disabled />
                  <span>Interface alarms</span>
                </label>
              </div>
            </div>
          </template>

          <template #interfaces>
            <div class="flex flex-col h-full min-h-0">
              <div class="flex flex-wrap items-end gap-2 mb-4 shrink-0">
                <UButton
                  v-if="isLocalDevice && authStore.canWrite"
                  label="New"
                  icon="i-lucide-plus"
                  size="sm"
                  color="neutral"
                  @click="openNewIface"
                />
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
                <span v-if="!isSupportedDriverPlatform" class="text-sm text-muted-color"
                  >Refresh/Update require an EOS, SROS-MD, IOS-XR, VRP or CISCOSMB device (this
                  device is "{{ device?.platform || 'unknown' }}").</span
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
                <template #vrf-cell="{ row }">
                  {{ row.original.vrf || '—' }}
                </template>

                <template #actions-cell="{ row }">
                  <div class="flex gap-1">
                    <UButton
                      icon="i-lucide-pencil"
                      variant="ghost"
                      color="neutral"
                      size="sm"
                      title="Edit interface"
                      @click="openEditIface(row.original)"
                    />
                    <UButton
                      v-if="authStore.canWrite && isLocalDevice && !row.original.netbox_id"
                      icon="i-lucide-trash"
                      variant="ghost"
                      color="error"
                      size="sm"
                      :loading="ifaceDeleting"
                      @click="removeIface(row.original)"
                    />
                  </div>
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
                    <UButton
                      v-if="authStore.canWrite"
                      icon="i-lucide-plus"
                      size="sm"
                      variant="ghost"
                      color="neutral"
                      title="Add service"
                      @click="openAttach(row.original)"
                    />
                  </div>
                </template>
                <template #addresses-cell="{ row }">
                  <div class="flex flex-wrap items-center gap-1">
                    <span
                      v-for="addr in row.original.addresses ?? []"
                      :key="addr.id"
                      class="inline-flex items-center gap-0.5 whitespace-nowrap text-sm"
                    >
                      <button
                        type="button"
                        class="hover:underline"
                        :title="authStore.canWrite && !addr.netbox_id ? 'Edit address' : ''"
                        @click="
                          authStore.canWrite && !addr.netbox_id
                            ? openEditAddr(row.original, addr)
                            : undefined
                        "
                      >
                        {{ addr.address }}
                      </button>
                      <UBadge
                        v-if="isManagementAddr(addr)"
                        color="info"
                        variant="subtle"
                        size="xs"
                      >
                        mgmt
                      </UBadge>
                      <UButton
                        v-if="authStore.canWrite && !addr.netbox_id"
                        icon="i-lucide-x"
                        size="xs"
                        variant="ghost"
                        color="error"
                        :loading="addrDeleting"
                        title="Remove address"
                        @click="removeAddr(addr)"
                      />
                    </span>
                    <UButton
                      v-if="authStore.canWrite"
                      icon="i-lucide-plus"
                      size="xs"
                      variant="ghost"
                      color="neutral"
                      title="Add IP address"
                      @click="openAddAddr(row.original)"
                    />
                  </div>
                </template>
              </UTable>
              <div v-if="authStore.opticalEnabled && isOpticalDevice" class="mt-4 shrink-0">
                <div class="font-bold mb-2">Cross-connects</div>
                <ul v-if="xconnects.length" class="mb-2">
                  <li v-for="x in xconnects" :key="x.id" class="flex items-center gap-2">
                    <span>
                      {{ xcKindLabels[x.kind] || x.kind }} ·
                      {{ interfaceNameById(x.interface_a_id) }} ↔
                      {{ interfaceNameById(x.interface_b_id) }}
                    </span>
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
                <p v-else class="text-sm text-muted-color mb-2">
                  No cross-connects on this device.
                </p>
                <div v-if="authStore.canWrite" class="flex flex-wrap gap-2 mt-2 items-center">
                  <USelectMenu
                    v-model="xcKind"
                    :items="xcKindItems"
                    value-key="value"
                    label-key="label"
                    class="w-48"
                  />
                  <USelectMenu
                    v-model="xcA"
                    :items="xcInterfaceItems"
                    value-key="value"
                    label-key="label"
                    placeholder="Port A"
                    class="min-w-64 w-72"
                  />
                  <USelectMenu
                    v-model="xcB"
                    :items="xcInterfaceItems"
                    value-key="value"
                    label-key="label"
                    placeholder="Port B"
                    class="min-w-64 w-72"
                  />
                  <UButton label="Add" :disabled="!xcA || !xcB" @click="addXConnect" />
                </div>
              </div>
            </div>
          </template>

          <template #vlans>
            <div class="flex flex-col h-full min-h-0">
              <p v-if="!isGlobalVlanPlatform" class="text-sm text-muted-color mb-3 shrink-0">
                VLAN assignment can be pushed on EOS, VRP and CISCOSMB devices (this device is "{{
                  device?.platform || 'unknown'
                }}").
              </p>
              <VlanEditDialog
                ref="vlanEditor"
                :interfaces="device?.interfaces ?? []"
                :device-id="device?.id"
                :device-name="device?.name"
                :platform="device?.platform"
                :can-save="authStore.canWrite && isGlobalVlanPlatform"
                @saved="onVlanSaved"
              />
            </div>
          </template>

          <template #oxidized>
            <div class="h-full min-h-0">
              <OxidizedNodePanel v-if="detailTab === 'oxidized'" :node-name="device.name" />
            </div>
          </template>
        </UTabs>
      </div>
    </template>

    <template #footer>
      <UButton
        v-if="isLocalDevice && authStore.canWrite"
        label="Delete"
        icon="i-lucide-trash"
        color="error"
        variant="ghost"
        :loading="deletingDevice"
        @click="removeLocalDevice"
      />
      <UButton
        v-if="isLocalDevice && authStore.canWrite"
        label="Save"
        icon="i-lucide-check"
        :loading="createSaving"
        @click="saveLocalDevice"
      />
      <UButton label="Close" icon="i-lucide-x" variant="ghost" @click="detailDialog = false" />
    </template>
  </FormModal>

  <FormModal
    v-model:open="createDialog"
    :source="createForm"
    title="New device"
    :ui="{ content: 'sm:max-w-lg' }"
  >
    <template #body>
      <div class="flex flex-col gap-4">
        <UFormField label="Name">
          <UInput v-model="createForm.name" class="w-full" autofocus />
        </UFormField>
        <UFormField label="Device type">
          <USelect v-model="createForm.device_type_id" :items="deviceTypeItems" class="w-full" />
        </UFormField>
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
    <template #footer>
      <UButton label="Cancel" icon="i-lucide-x" variant="ghost" @click="createDialog = false" />
      <UButton label="Create" icon="i-lucide-check" :loading="createSaving" @click="saveNew" />
    </template>
  </FormModal>

  <FormModal
    v-model:open="ifaceFormOpen"
    :source="ifaceForm"
    :title="ifaceDialogTitle"
    :ui="{ content: 'sm:max-w-sm' }"
  >
    <template #body>
      <div class="flex flex-col gap-4">
        <UFormField label="Name">
          <UInput
            v-model="ifaceForm.name"
            class="w-full font-mono"
            autofocus
            :disabled="!!ifaceEditingId && !ifaceFormWritable"
          />
        </UFormField>
        <UFormField label="Type">
          <USelect
            v-model="ifaceForm.type"
            :items="interfaceTypeItems"
            class="w-full"
            :disabled="!!ifaceEditingId && !ifaceFormWritable"
          />
        </UFormField>
        <UFormField label="Label">
          <UInput v-model="ifaceForm.label" class="w-full" :disabled="!!ifaceEditingId && !ifaceFormWritable" />
        </UFormField>
        <UFormField label="Description">
          <UInput
            v-model="ifaceForm.description"
            class="w-full"
            :disabled="!!ifaceEditingId && !ifaceFormWritable"
          />
        </UFormField>
        <UFormField label="VRF">
          <UInput v-model="ifaceForm.vrf" class="w-full" :disabled="!!ifaceEditingId && !ifaceFormWritable" />
        </UFormField>
        <UFormField label="Enabled">
          <USwitch v-model="ifaceForm.enabled" :disabled="!!ifaceEditingId && !ifaceFormWritable" />
        </UFormField>
      </div>
    </template>
    <template #footer>
      <UButton label="Cancel" icon="i-lucide-x" variant="ghost" @click="ifaceFormOpen = false" />
      <UButton
        v-if="authStore.canWrite && ifaceFormWritable"
        :label="ifaceEditingId ? 'Save' : 'Create'"
        icon="i-lucide-check"
        :loading="ifaceSaving"
        @click="saveIface"
      />
    </template>
  </FormModal>

  <FormModal
    v-model:open="addrFormOpen"
    :source="addrForm"
    :title="addrDialogTitle"
    :ui="{ content: 'sm:max-w-sm' }"
  >
    <template #body>
      <div class="flex flex-col gap-4">
        <UFormField label="Address">
          <div class="flex gap-2">
            <UInput
              v-model="addrForm.address"
              class="w-full font-mono"
              placeholder="10.0.0.1/24"
              autofocus
            />
            <UButton
              v-if="authStore.ipamEnabled"
              label="Pick"
              icon="i-lucide-layout-grid"
              color="neutral"
              variant="outline"
              @click="addrPickerOpen = true"
            />
          </div>
        </UFormField>
        <UFormField label="DNS name">
          <UInput v-model="addrForm.dns_name" class="w-full" />
        </UFormField>
        <UFormField label="VRF">
          <UInput v-model="addrForm.vrf" class="w-full" />
        </UFormField>
        <UFormField label="Role">
          <UInput v-model="addrForm.role" class="w-full" />
        </UFormField>
        <UCheckbox v-model="addrForm.management" label="Management IP address" />
      </div>
    </template>
    <template #footer>
      <UButton label="Cancel" icon="i-lucide-x" variant="ghost" @click="addrFormOpen = false" />
      <UButton
        :label="addrEditingId ? 'Save' : 'Add'"
        icon="i-lucide-check"
        :loading="addrSaving"
        @click="saveNewAddr"
      />
    </template>
  </FormModal>

  <IpamAddressPicker v-model:open="addrPickerOpen" :vrf="addrPickerVrf" @select="onPickAddr" />

  <ServiceEditDialog
    v-model:open="serviceDialogOpen"
    :service-id="editingServiceId"
    @saved="reloadDeviceInterfaces"
    @deleted="reloadDeviceInterfaces"
  />

  <AttachServiceDialog
    v-model:open="attachOpen"
    :device-id="attachTarget?.deviceId"
    :device-name="attachTarget?.deviceName"
    :interface-id="attachTarget?.interfaceId"
    :interface-name="attachTarget?.interfaceName"
    @attached="reloadDeviceInterfaces"
  />

  <SiteSelector
    v-model:visible="siteSelectorVisible"
    :selected-name="createForm.site"
    @select="onSiteSelected"
  />
</template>
