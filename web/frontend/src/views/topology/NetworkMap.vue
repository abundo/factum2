<script setup>
import { MapboxOverlay } from '@deck.gl/mapbox'
import { ArcLayer, ScatterplotLayer, TextLayer } from '@deck.gl/layers'
import { Map as MaplibreMap, NavigationControl, setWorkerUrl } from 'maplibre-gl'
import 'maplibre-gl/dist/maplibre-gl.css'
import { useToast } from '@nuxt/ui/composables'
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import {
  assignDeviceLocation,
  getTopology,
  getTopologyDevices,
  reverseGeocode,
} from '@/api/topology'
import { useAuthStore } from '@/stores/auth'
import SiteAssignPanel from './SiteAssignPanel.vue'

const toast = useToast()
const authStore = useAuthStore()

// Role filter and basemap, remembered per-browser (not per-user account -
// the map has no server-side per-user settings store) so they survive reloads.
const DEFAULT_ROLES_STORAGE_KEY = 'networkMap.defaultRoles'
const BASEMAP_STORAGE_KEY = 'networkMap.basemap'

function readDefaultRoles() {
  try {
    const raw = localStorage.getItem(DEFAULT_ROLES_STORAGE_KEY)
    return raw ? JSON.parse(raw) : null
  } catch {
    return null
  }
}

function readBasemap() {
  try {
    const raw = localStorage.getItem(BASEMAP_STORAGE_KEY)
    return raw === 'dark' || raw === 'light' ? raw : 'light'
  } catch {
    return 'light'
  }
}

const BASEMAP_STYLES = {
  // VersaTiles Colorful on Shortbread vector tiles - OSM Carto-like
  // (cream land, pale-yellow roads, olive parks, light-blue water), no
  // API key. Needs outbound access to tiles.versatiles.org; swap for a
  // self-hosted Shortbread source if the deployment network doesn't have
  // that. OSMF's own vector.openstreetmap.org is the same schema but
  // donation-funded with a usage policy that production apps shouldn't hit.
  light: 'https://tiles.versatiles.org/assets/styles/colorful/style.json',
  // CARTO Dark Matter - no API key, needs outbound access to
  // basemaps.cartocdn.com. High contrast for the colored device dots and
  // cyan connection arcs.
  dark: 'https://basemaps.cartocdn.com/gl/dark-matter-gl-style/style.json',
}

const basemapItems = [
  { label: 'Light', value: 'light' },
  { label: 'Dark', value: 'dark' },
]
const basemap = ref(readBasemap())

// Vite/Rolldown doesn't statically detect maplibre-gl's internal
// `new Worker(new URL('./maplibre-gl-worker.mjs', import.meta.url))` the
// way Rollup/webpack do, so that worker chunk never gets emitted into the
// build - the default worker URL 404s (falling through to the SPA's HTML
// fallback, which the browser then rejects as a module script with a
// MIME-type error) and the map silently never leaves its "loading" state.
// Point it instead at a copy of the same file served as a plain static
// asset (see package.json's predev/prebuild scripts) - that copy must
// keep maplibre-gl-shared.mjs alongside it, since the worker file's own
// (unprocessed, relative) `import ... from "./maplibre-gl-shared.mjs"`
// resolves against wherever it's served from, not the Vite build graph.
setWorkerUrl('/maplibre-gl-worker.mjs')

const mapContainer = ref(null)
const loading = ref(true)
const error = ref(null)
const selected = ref(null)
const hoverInfo = ref(null)

const assignMode = ref(false)
const allDevices = ref([])
const allDevicesLoading = ref(false)
const assignSelected = ref(null)
const pickingCoords = ref(false)
const pickedCoords = ref(null)
const pickedAddress = ref('')
const pickedAddressLoading = ref(false)
const assignSaving = ref(false)
let geocodeGen = 0

// Raw API response, kept around unfiltered so toggling a role filter never
// needs to re-fetch - only rebuild() below, which re-derives the laid-out
// devices/edges/layers from these plus `activeRoles`.
const rawDevices = ref([])
const rawEdges = ref([])
// Sites are plotted unconditionally, independent of the role filter - a
// site is a location, not a device, so it has no role to filter by.
const rawSites = ref([])
const activeRoles = ref(new Set())
const opticalOnly = ref(false)

const availableRoles = computed(() =>
  [...new Set(rawDevices.value.map((d) => d.role || 'Unassigned'))].sort(),
)

let map = null
let overlay = null
// Bumped on every setStyle so a slow previous style.load can't restore
// an older camera/overlay after the user has already picked a newer one.
let styleGen = 0

// Hovering shouldn't pop up info instantly - only once the pointer has
// rested on the same device/cable for a bit. `pending` tracks whatever's
// currently under the pointer (updated on every move, even before the
// delay elapses, so the tooltip lands at the cursor's latest position
// rather than where it was when the hover started) while `hoverTimer` is
// the one in-flight "reveal" callback; a hover onto a *different* object
// cancels it and restarts the wait instead of letting a stale one fire.
const HOVER_DELAY_MS = 400
let hoverTimer = null
let pending = null

function handleHover(kind, { object, x, y }) {
  if (!object) {
    if (hoverTimer) clearTimeout(hoverTimer)
    hoverTimer = null
    pending = null
    hoverInfo.value = null
    return
  }

  const isSameObject = pending?.kind === kind && pending.object === object
  pending = { kind, object, x, y }

  if (isSameObject) {
    if (hoverInfo.value) hoverInfo.value = pending
    return
  }

  if (hoverTimer) clearTimeout(hoverTimer)
  hoverInfo.value = null
  hoverTimer = setTimeout(() => {
    hoverInfo.value = pending
    hoverTimer = null
  }, HOVER_DELAY_MS)
}

const STATUS_COLORS = {
  active: [34, 197, 94],
  offline: [239, 68, 68],
  failed: [239, 68, 68],
  decommissioning: [239, 68, 68],
  planned: [234, 179, 8],
  staged: [234, 179, 8],
}

// Overlay colors follow the basemap: light-on-dark pills for Dark Matter,
// dark-on-white for the OSM-like light style. Device status fills stay the
// same in both so the legend above the map doesn't have to switch.
const OVERLAY_PALETTES = {
  light: {
    other: [100, 116, 139],
    siteRing: [71, 85, 105, 220],
    arc: [2, 132, 199, 210],
    arcWidth: 2,
    deviceStroke: [51, 65, 85],
    deviceStrokeWidth: 1.5,
    labelText: [30, 41, 59],
    labelBg: [255, 255, 255, 230],
    siteLabelText: [71, 85, 105],
    siteLabelBg: [255, 255, 255, 230],
    edgeLabelText: [3, 105, 161],
  },
  dark: {
    other: [148, 163, 184],
    siteRing: [148, 163, 184, 200],
    arc: [56, 189, 248, 160],
    arcWidth: 1.5,
    deviceStroke: [15, 23, 42],
    deviceStrokeWidth: 1,
    labelText: [226, 232, 240],
    labelBg: [15, 23, 42, 160],
    siteLabelText: [148, 163, 184, 220],
    siteLabelBg: [15, 23, 42, 130],
    edgeLabelText: [125, 211, 252, 230],
  },
}

function overlayPalette() {
  return OVERLAY_PALETTES[basemap.value] ?? OVERLAY_PALETTES.light
}

function statusColor(status) {
  return STATUS_COLORS[(status ?? '').toLowerCase()] ?? overlayPalette().other
}

// Devices with no coordinates of their own inherit their site's (see
// models.Device.Latitude/Longitude), so every device at one site starts
// out on the exact same point. Fan same-point devices out in a small
// circle around that point instead of leaving them stacked, so a site
// with many devices still reads as a distinct cluster on the map.
function layoutDevices(devices) {
  const groups = new Map()
  for (const d of devices) {
    const key = `${d.latitude.toFixed(4)},${d.longitude.toFixed(4)}`
    if (!groups.has(key)) groups.set(key, [])
    groups.get(key).push(d)
  }

  const out = []
  for (const group of groups.values()) {
    const n = group.length
    group.forEach((d, i) => {
      if (n === 1) {
        out.push({ ...d, mapLat: d.latitude, mapLng: d.longitude })
        return
      }
      const angle = (2 * Math.PI * i) / n
      const radiusDeg = (0.0004 / 3) * Math.min(3 + n, 15)
      const latRad = (d.latitude * Math.PI) / 180
      out.push({
        ...d,
        mapLat: d.latitude + radiusDeg * Math.sin(angle),
        mapLng: d.longitude + (radiusDeg * Math.cos(angle)) / Math.cos(latRad),
      })
    })
  }
  return out
}

function buildLayers(devices, edges, sites) {
  const palette = overlayPalette()
  const byID = new Map(devices.map((d) => [d.id, d]))

  const arcs = edges
    .map((e) => {
      const a = byID.get(e.device_a_id)
      const b = byID.get(e.device_b_id)
      return a && b
        ? {
            ...e,
            source: [a.mapLng, a.mapLat],
            target: [b.mapLng, b.mapLat],
            // Great-circle midpoint would need slerp to be strictly correct,
            // but every connection here spans a short enough distance that
            // the plain lng/lat average reads as "the middle of the line".
            midpoint: [(a.mapLng + b.mapLng) / 2, (a.mapLat + b.mapLat) / 2],
            deviceAName: a.name,
            deviceBName: b.name,
          }
        : null
    })
    .filter((e) => e !== null)
  const labeledArcs = arcs.filter((e) => e.label)

  return [
    // Sites render as a hollow ring beneath everything else, so a site with
    // devices still shows its ring (drawn under their dots) and a site with
    // none is still visible - the network map otherwise has nowhere to
    // place a site with no device of its own (see TopologySiteDTO).
    new ScatterplotLayer({
      id: 'sites',
      data: sites,
      pickable: true,
      stroked: true,
      filled: false,
      radiusUnits: 'pixels',
      getPosition: (d) => [d.mapLng, d.mapLat],
      getLineColor: palette.siteRing,
      lineWidthMinPixels: 1.5,
      getRadius: 12,
      radiusMinPixels: 10,
      radiusMaxPixels: 16,
      onHover: (info) => handleHover('site', info),
    }),
    new ArcLayer({
      id: 'connections',
      data: arcs,
      pickable: true,
      getSourcePosition: (d) => d.source,
      getTargetPosition: (d) => d.target,
      getSourceColor: palette.arc,
      getTargetColor: palette.arc,
      getWidth: palette.arcWidth,
      getHeight: 0,
      greatCircle: true,
      onHover: (info) => handleHover('edge', info),
    }),
    new ScatterplotLayer({
      id: 'devices',
      data: devices,
      pickable: true,
      stroked: true,
      radiusUnits: 'pixels',
      getPosition: (d) => [d.mapLng, d.mapLat],
      getFillColor: (d) => statusColor(d.status),
      getLineColor: palette.deviceStroke,
      lineWidthMinPixels: palette.deviceStrokeWidth,
      getRadius: (d) => (highlightedDeviceId() === d.id ? 9 : 6),
      radiusMinPixels: 5,
      radiusMaxPixels: 12,
      getLineWidth: (d) => (highlightedDeviceId() === d.id ? 2.5 : palette.deviceStrokeWidth),
      onClick: ({ object }) => {
        selected.value = object ?? null
        if (assignMode.value && object) {
          const full = allDevices.value.find((d) => d.id === object.id) ?? object
          selectAssignDevice(full, { fromMap: true })
        }
      },
      onHover: (info) => handleHover('device', info),
    }),
    new TextLayer({
      id: 'site-labels',
      data: sites,
      getPosition: (d) => [d.mapLng, d.mapLat],
      getText: (d) => d.name,
      getColor: palette.siteLabelText,
      getSize: 11,
      getPixelOffset: [0, -16],
      background: true,
      getBackgroundColor: palette.siteLabelBg,
      backgroundPadding: [4, 2],
      fontFamily: '"Helvetica Neue", Arial, sans-serif',
    }),
    new TextLayer({
      id: 'edge-labels',
      data: labeledArcs,
      getPosition: (d) => d.midpoint,
      getText: (d) => d.label,
      getColor: palette.edgeLabelText,
      getSize: 11,
      background: true,
      getBackgroundColor: palette.labelBg,
      backgroundPadding: [4, 2],
      fontFamily: '"Helvetica Neue", Arial, sans-serif',
    }),
    new TextLayer({
      id: 'device-labels',
      data: devices,
      getPosition: (d) => [d.mapLng, d.mapLat],
      getText: (d) => d.name,
      getColor: palette.labelText,
      getSize: 12,
      getPixelOffset: [0, 14],
      background: true,
      getBackgroundColor: palette.labelBg,
      backgroundPadding: [4, 2],
      fontFamily: '"Helvetica Neue", Arial, sans-serif',
    }),
    ...(pickedCoords.value
      ? [
          new ScatterplotLayer({
            id: 'pick-pin',
            data: [pickedCoords.value],
            pickable: false,
            stroked: true,
            filled: true,
            radiusUnits: 'pixels',
            getPosition: (d) => [d.lng, d.lat],
            getFillColor: [249, 115, 22],
            getLineColor: [154, 52, 18],
            lineWidthMinPixels: 1.5,
            getRadius: 8,
            radiusMinPixels: 7,
            radiusMaxPixels: 12,
          }),
        ]
      : []),
  ]
}

function highlightedDeviceId() {
  return assignSelected.value?.id ?? selected.value?.id ?? null
}

// Sites have their own already-resolved coordinates (no fan-out needed the
// way same-point devices get, see layoutDevices) - just give them the same
// mapLat/mapLng shape buildLayers/fitToPoints expect from a device.
function layoutSites(sites) {
  return sites.map((s) => ({ ...s, mapLat: s.latitude, mapLng: s.longitude }))
}

// Fits the map to every device and site currently on it, so a site with no
// devices of its own is still within the initial view rather than only
// ones a device happens to be plotted at.
function fitToPoints(devices, sites) {
  const points = [...devices, ...sites]
  if (!points.length) return
  let minLat = Infinity
  let maxLat = -Infinity
  let minLng = Infinity
  let maxLng = -Infinity
  for (const p of points) {
    minLat = Math.min(minLat, p.mapLat)
    maxLat = Math.max(maxLat, p.mapLat)
    minLng = Math.min(minLng, p.mapLng)
    maxLng = Math.max(maxLng, p.mapLng)
  }
  map.fitBounds(
    [
      [minLng, minLat],
      [maxLng, maxLat],
    ],
    { padding: 60, duration: 0, maxZoom: 12 },
  )
}

// Re-derives the map from `rawDevices`/`rawEdges`/`rawSites` filtered down
// to `activeRoles` (sites are unaffected by the role filter - see
// `rawSites`). Devices hidden by the filter are dropped before
// layoutDevices runs, not after - so a site's fan-out radius (driven by how
// many devices land on the same point, see layoutDevices) shrinks to match
// what's actually visible instead of the site's full device count.
function rebuild() {
  const devices = rawDevices.value.filter((d) => {
    if (!activeRoles.value.has(d.role || 'Unassigned')) return false
    if (opticalOnly.value && !d.optical_kind) return false
    return true
  })
  const laidOutDevices = layoutDevices(devices)
  const laidOutSites = layoutSites(rawSites.value)
  overlay?.setProps({ layers: buildLayers(laidOutDevices, rawEdges.value, laidOutSites) })
  return { devices: laidOutDevices, sites: laidOutSites }
}

function toggleRole(role) {
  if (activeRoles.value.has(role)) {
    activeRoles.value.delete(role)
  } else {
    activeRoles.value.add(role)
  }
  rebuild()
}

function showAllRoles() {
  activeRoles.value = new Set(availableRoles.value)
  rebuild()
}

function toggleOpticalOnly() {
  opticalOnly.value = !opticalOnly.value
  rebuild()
}

function applyBasemapStyle() {
  if (!map) return
  const gen = ++styleGen
  const camera = {
    center: map.getCenter(),
    zoom: map.getZoom(),
    pitch: map.getPitch(),
    bearing: map.getBearing(),
  }
  map.setStyle(BASEMAP_STYLES[basemap.value])
  map.once('style.load', () => {
    if (gen !== styleGen || !map) return
    map.jumpTo(camera)
    rebuild()
  })
}

function onBasemapChange() {
  try {
    localStorage.setItem(BASEMAP_STORAGE_KEY, basemap.value)
  } catch {
    // Same as saveDefaultRoles: map still switches, just won't persist.
  }
  applyBasemapStyle()
}

function saveDefaultRoles() {
  try {
    localStorage.setItem(DEFAULT_ROLES_STORAGE_KEY, JSON.stringify([...activeRoles.value]))
    toast.add({
      color: 'success',
      title: 'Saved',
      description: 'Role filter saved as default.',
      duration: 3000,
    })
  } catch {
    toast.add({
      color: 'error',
      title: 'Error',
      description: "Couldn't save the role filter - browser storage is unavailable.",
      duration: 3000,
    })
  }
}

function loadTopology() {
  loading.value = true
  error.value = null
  getTopology()
    .then((data) => {
      rawDevices.value = data.devices ?? []
      rawEdges.value = data.edges ?? []
      rawSites.value = data.sites ?? []
      // A saved default only applies to roles that still exist - a role
      // dropped from Netbox since the save shouldn't silently hide devices
      // that no longer have any way to be un-filtered from the UI.
      const savedRoles = readDefaultRoles()?.filter((r) => availableRoles.value.includes(r))
      activeRoles.value = new Set(savedRoles?.length ? savedRoles : availableRoles.value)
      const { devices, sites } = rebuild()
      fitToPoints(devices, sites)
    })
    .catch(() => {
      error.value = 'Failed to load network topology.'
    })
    .finally(() => {
      loading.value = false
    })
}

function loadAllDevices() {
  allDevicesLoading.value = true
  return getTopologyDevices()
    .then((data) => {
      allDevices.value = data.devices ?? []
      if (assignSelected.value) {
        assignSelected.value =
          allDevices.value.find((d) => d.id === assignSelected.value.id) ?? assignSelected.value
      }
    })
    .catch(() => {
      toast.add({
        color: 'error',
        title: 'Error',
        description: 'Failed to load devices for site assignment.',
        duration: 4000,
      })
    })
    .finally(() => {
      allDevicesLoading.value = false
    })
}

function panTo(lat, lng) {
  if (!map || lat == null || lng == null) return
  map.flyTo({
    center: [lng, lat],
    zoom: Math.max(map.getZoom(), 10),
    duration: 800,
  })
}

function setPicking(on) {
  pickingCoords.value = on
  if (map) {
    map.getCanvas().style.cursor = on ? 'crosshair' : ''
  }
}

function hasMappableCoords(d) {
  return d?.latitude != null && d?.longitude != null
}

function clearPickedAddress() {
  geocodeGen += 1
  pickedAddress.value = ''
  pickedAddressLoading.value = false
}

function lookupPickedAddress(lat, lng) {
  const gen = ++geocodeGen
  pickedAddress.value = ''
  pickedAddressLoading.value = true
  reverseGeocode(lat, lng)
    .then((data) => {
      if (gen !== geocodeGen) return
      pickedAddress.value = data?.address ?? ''
    })
    .catch(() => {
      if (gen !== geocodeGen) return
      pickedAddress.value = ''
    })
    .finally(() => {
      if (gen === geocodeGen) pickedAddressLoading.value = false
    })
}

function selectAssignDevice(device, { fromMap = false } = {}) {
  assignSelected.value = device
  selected.value = fromMap ? selected.value : null
  clearPickedAddress()
  pickedCoords.value = hasMappableCoords(device)
    ? { lat: device.latitude, lng: device.longitude }
    : null
  if (hasMappableCoords(device)) {
    panTo(device.latitude, device.longitude)
    setPicking(false)
  } else {
    setPicking(authStore.canWrite)
  }
  rebuild()
}

function toggleAssignMode() {
  assignMode.value = !assignMode.value
  if (assignMode.value) {
    loadAllDevices()
  } else {
    setPicking(false)
    pickedCoords.value = null
    clearPickedAddress()
    assignSelected.value = null
    rebuild()
  }
  nextTick(() => map?.resize())
}

function onUseSite(site) {
  clearPickedAddress()
  pickedCoords.value = { lat: site.latitude, lng: site.longitude }
  setPicking(false)
  panTo(site.latitude, site.longitude)
  rebuild()
}

function onAssign({ site_name, latitude, longitude, physical_address }) {
  const device = assignSelected.value
  if (!device) return
  assignSaving.value = true
  const body = { latitude, longitude }
  if (site_name) body.site_name = site_name
  if (physical_address) body.physical_address = physical_address
  assignDeviceLocation(device.id, body)
    .then((data) => {
      toast.add({
        color: 'success',
        title: 'Assigned',
        description: site_name
          ? `${device.name} → ${data.site?.name ?? site_name} in NetBox.`
          : `Coordinates saved on ${device.name} in NetBox.`,
        duration: 4000,
      })
      if (data.device) {
        allDevices.value = allDevices.value.map((d) => (d.id === data.device.id ? data.device : d))
        assignSelected.value = data.device
      }
      if (data.site?.id) {
        const rest = rawSites.value.filter(
          (s) => s.id !== data.site.id && s.name !== data.site.name,
        )
        rawSites.value = [...rest, data.site]
      }
      pickedCoords.value = { lat: latitude, lng: longitude }
      setPicking(false)
      return getTopology().then((topo) => {
        rawDevices.value = topo.devices ?? []
        rawEdges.value = topo.edges ?? []
        rawSites.value = topo.sites ?? []
        rebuild()
        panTo(latitude, longitude)
      })
    })
    .catch((err) => {
      toast.add({
        color: 'error',
        title: 'Could not save location',
        description: err.response?.data?.error ?? err.message ?? 'Request failed.',
        duration: 5000,
      })
    })
    .finally(() => {
      assignSaving.value = false
    })
}

watch([assignSelected, pickedCoords], () => {
  if (overlay) rebuild()
})

onMounted(() => {
  map = new MaplibreMap({
    container: mapContainer.value,
    style: BASEMAP_STYLES[basemap.value],
    center: [15, 58],
    zoom: 3,
    pitch: 0,
    antialias: true,
  })
  map.addControl(new NavigationControl({ visualizePitch: true }), 'top-right')

  overlay = new MapboxOverlay({ layers: [] })
  map.addControl(overlay)

  map.on('click', (e) => {
    if (!pickingCoords.value) return
    pickedCoords.value = { lat: e.lngLat.lat, lng: e.lngLat.lng }
    lookupPickedAddress(e.lngLat.lat, e.lngLat.lng)
    rebuild()
  })

  map.on('load', loadTopology)
})

onBeforeUnmount(() => {
  styleGen += 1
  map?.remove()
  map = null
  overlay = null
})
</script>

<template>
  <div class="card flex min-h-0 flex-1 flex-col">
    <div class="flex flex-wrap gap-2 items-center justify-between mb-4 shrink-0">
      <div class="flex flex-wrap items-center gap-4">
        <h4 class="m-0">Network map</h4>
        <URadioGroup
          v-model="basemap"
          :items="basemapItems"
          orientation="horizontal"
          size="sm"
          @update:model-value="onBasemapChange"
        />
        <UButton
          label="Assign locations"
          size="xs"
          color="primary"
          icon="i-lucide-map-pin"
          :variant="assignMode ? 'solid' : 'outline'"
          @click="toggleAssignMode"
        />
      </div>
      <div class="flex items-center gap-3 text-sm text-muted-color">
        <span class="flex items-center gap-1">
          <span class="size-2.5 rounded-full" style="background: rgb(34 197 94)" />Active
        </span>
        <span class="flex items-center gap-1">
          <span class="size-2.5 rounded-full" style="background: rgb(239 68 68)" />Offline/failed
        </span>
        <span class="flex items-center gap-1">
          <span class="size-2.5 rounded-full" style="background: rgb(234 179 8)" />Planned/staged
        </span>
        <span class="flex items-center gap-1">
          <span class="size-2.5 rounded-full" style="background: rgb(100 116 139)" />Other
        </span>
      </div>
    </div>

    <UAlert v-if="error" color="error" variant="subtle" :title="error" class="mb-4 shrink-0" />

    <div class="flex flex-wrap gap-2 items-center mb-4 shrink-0">
      <UButton
        v-if="availableRoles.length"
        label="All"
        size="xs"
        color="primary"
        :variant="activeRoles.size === availableRoles.length ? 'solid' : 'outline'"
        @click="showAllRoles"
      />
      <UButton
        v-if="availableRoles.length"
        label="Optical only"
        size="xs"
        color="primary"
        :variant="opticalOnly ? 'solid' : 'outline'"
        @click="toggleOpticalOnly"
      />
      <UButton
        v-for="role in availableRoles"
        :key="role"
        :label="role"
        size="xs"
        color="primary"
        :variant="activeRoles.has(role) ? 'solid' : 'outline'"
        @click="toggleRole(role)"
      />
      <UButton
        v-if="availableRoles.length"
        label="Save as default"
        icon="i-lucide-save"
        size="xs"
        color="neutral"
        variant="ghost"
        class="ml-auto"
        @click="saveDefaultRoles"
      />
    </div>

    <div class="flex min-h-0 flex-1 gap-3">
      <SiteAssignPanel
        v-if="assignMode"
        :devices="allDevices"
        :sites="rawSites"
        :selected-id="assignSelected?.id ?? null"
        :can-write="authStore.canWrite"
        :picking="pickingCoords"
        :picked="pickedCoords"
        :address="pickedAddress"
        :address-loading="pickedAddressLoading"
        :saving="assignSaving"
        :loading="allDevicesLoading"
        @select="selectAssignDevice"
        @update:picking="setPicking"
        @use-site="onUseSite"
        @assign="onAssign"
      />
      <div
        class="relative min-h-0 flex-1 rounded-lg overflow-hidden border border-default"
        :class="pickingCoords ? 'cursor-crosshair ring-2 ring-primary' : ''"
      >
        <!--
        w-full h-full, not absolute inset-0: maplibre-gl.css sets
        `.maplibregl-map { position: relative }` on this exact element
        (the class it adds to the container it's given), which wins the
        cascade over Tailwind's `.absolute` here since maplibre's
        stylesheet loads after Tailwind's - collapsing it to 0 height
        (top/bottom:0 on a `position:relative` box doesn't size it the way
        it would under `absolute`). Percentage sizing off the parent's
        explicit height sidesteps the conflict instead of fighting it.
      -->
        <div ref="mapContainer" class="w-full h-full" />

        <div v-if="loading" class="absolute inset-0 flex items-center justify-center bg-default/60">
          <UIcon name="i-lucide-loader-2" class="size-8 animate-spin" />
        </div>

        <div
          v-if="pickingCoords"
          class="absolute top-3 left-3 z-10 rounded-lg border border-default bg-default px-3 py-2 text-sm shadow-lg"
        >
          Click the map to set latitude and longitude.
        </div>

        <div
          v-if="selected && !assignMode"
          class="absolute top-3 left-3 z-10 w-64 rounded-lg border border-default bg-default p-3 shadow-lg"
        >
          <div class="flex items-start justify-between gap-2 mb-2">
            <div class="font-medium">{{ selected.name }}</div>
            <UButton
              icon="i-lucide-x"
              size="xs"
              color="neutral"
              variant="ghost"
              @click="selected = null"
            />
          </div>
          <div class="text-sm text-muted-color space-y-1">
            <div>Site: {{ selected.site || '-' }}</div>
            <div>Role: {{ selected.role || '-' }}</div>
            <div>Status: {{ selected.status || '-' }}</div>
          </div>
        </div>

        <div
          v-if="hoverInfo"
          class="absolute z-20 max-w-64 rounded-md border border-default bg-default px-2.5 py-1.5 text-xs shadow-lg pointer-events-none"
          :style="{ left: `${hoverInfo.x + 12}px`, top: `${hoverInfo.y + 12}px` }"
        >
          <template v-if="hoverInfo.kind === 'device'">
            <div class="font-medium">{{ hoverInfo.object.name }}</div>
            <div class="text-muted-color">
              {{ hoverInfo.object.site || '-' }} · {{ hoverInfo.object.role || '-' }} ·
              {{ hoverInfo.object.status || '-' }}
            </div>
          </template>
          <template v-else-if="hoverInfo.kind === 'site'">
            <div class="font-medium">{{ hoverInfo.object.name }}</div>
          </template>
          <template v-else>
            <div class="font-medium">
              {{ hoverInfo.object.deviceAName
              }}<span v-if="hoverInfo.object.interface_a">
                ({{ hoverInfo.object.interface_a }})</span
              >
            </div>
            <div class="text-muted-color">
              {{ hoverInfo.object.deviceBName
              }}<span v-if="hoverInfo.object.interface_b">
                ({{ hoverInfo.object.interface_b }})</span
              >
            </div>
            <div v-if="hoverInfo.object.label" class="text-muted-color">
              Label: {{ hoverInfo.object.label }}
            </div>
          </template>
        </div>
      </div>
    </div>
  </div>
</template>
