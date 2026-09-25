<script setup>
import { MapboxOverlay } from '@deck.gl/mapbox'
import { ArcLayer, ScatterplotLayer, TextLayer } from '@deck.gl/layers'
import { Map as MaplibreMap, NavigationControl, setWorkerUrl } from 'maplibre-gl'
import 'maplibre-gl/dist/maplibre-gl.css'
import { useToast } from '@nuxt/ui/composables'
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import {
  assignDeviceLocation,
  assignSiteLocation,
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
  // OpenFreeMap Liberty: OpenStreetMap vector tiles with buildings, streets,
  // and points of interest. No API key. Needs outbound access to
  // tiles.openfreemap.org. MapLibre shows the style's attribution.
  light: 'https://tiles.openfreemap.org/styles/liberty',
  // CARTO Dark Matter - no API key, needs outbound access to
  // basemaps.cartocdn.com. High contrast for the colored device dots and
  // the fiber / wavelength / capacity arcs.
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
const mapWrap = ref(null)
const loading = ref(true)
const error = ref(null)
const selected = ref(null)
const hoverInfo = ref(null)

const assignMode = ref(false)
const allDevices = ref([])
const allDevicesLoading = ref(false)
const assignSelected = ref(null)
const assignSelectedSite = ref(null)
const allSites = ref([])
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
const rawServiceLinks = ref([])
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
const EDGE_HOVER_DELAY_MS = 200
// Extra pixels around the pointer so thin connection arcs are hittable
// without drawing them that wide. Pair with the invisible hit ArcLayer.
const PICKING_RADIUS_PX = 10
const CONNECTION_HIT_WIDTH_PX = 16
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
  hoverTimer = setTimeout(
    () => {
      hoverInfo.value = pending
      hoverTimer = null
    },
    kind === 'edge' ? EDGE_HOVER_DELAY_MS : HOVER_DELAY_MS,
  )
}

function siteLabel(site) {
  if (!site || site === 'Default') return 'No site'
  return site
}

function hardwareLabel(d) {
  return [d?.manufacturer, d?.model_name].filter((s) => !!(s && String(s).trim())).join(' · ')
}

const hoverPos = computed(() => {
  if (!hoverInfo.value) return null
  const w = mapWrap.value?.clientWidth ?? 0
  const h = mapWrap.value?.clientHeight ?? 0
  const x = hoverInfo.value.x
  const y = hoverInfo.value.y
  const flipX = w > 0 && x > w * 0.62
  const flipY = h > 0 && y > h * 0.72
  return {
    left: `${flipX ? x - 12 : x + 12}px`,
    top: `${flipY ? y - 12 : y + 12}px`,
    transform: `translate(${flipX ? '-100%' : '0'}, ${flipY ? '-100%' : '0'})`,
  }
})

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
    arcWidth: 2,
    deviceStroke: [51, 65, 85],
    deviceStrokeWidth: 1.5,
    labelText: [30, 41, 59],
    labelBg: [255, 255, 255, 230],
    siteLabelText: [71, 85, 105],
    siteLabelBg: [255, 255, 255, 230],
    edgeLabelText: [71, 85, 105],
  },
  dark: {
    other: [148, 163, 184],
    siteRing: [148, 163, 184, 200],
    arcWidth: 1.5,
    deviceStroke: [15, 23, 42],
    deviceStrokeWidth: 1,
    labelText: [226, 232, 240],
    labelBg: [15, 23, 42, 160],
    siteLabelText: [148, 163, 184, 220],
    siteLabelBg: [15, 23, 42, 130],
    edgeLabelText: [203, 213, 225, 230],
  },
}

// Line colors follow the service riding the link. LF and LI share fiber,
// VL and VI share wavelength, CN and CI share capacity. A cable with no
// optical hop stays neutral; one used by both fiber and wavelength is mixed.
const LINK_COLORS = {
  cable: {
    light: [100, 116, 139, 210],
    dark: [148, 163, 184, 200],
  },
  fiber: {
    light: [217, 119, 6, 235],
    dark: [251, 191, 36, 235],
  },
  wavelength: {
    light: [147, 51, 234, 235],
    dark: [216, 180, 254, 235],
  },
  capacity: {
    light: [2, 132, 199, 235],
    dark: [56, 189, 248, 235],
  },
  mixed: {
    light: [190, 18, 60, 235],
    dark: [251, 113, 133, 235],
  },
}

const LINK_LEGEND = [
  { kind: 'cable', label: 'Cable' },
  { kind: 'fiber', label: 'Fiber (LF/LI)' },
  { kind: 'wavelength', label: 'Wavelength (VL/VI)' },
  { kind: 'capacity', label: 'Capacity (CN/CI)' },
]

function linkColor(kind) {
  const mode = basemap.value === 'dark' ? 'dark' : 'light'
  return (LINK_COLORS[kind] ?? LINK_COLORS.cable)[mode]
}

function linkSwatch(kind) {
  const [r, g, b] = linkColor(kind)
  return `rgb(${r} ${g} ${b})`
}

function kindLabel(kind) {
  switch (kind) {
    case 'fiber':
      return 'Fiber (LF/LI)'
    case 'wavelength':
      return 'Wavelength (VL/VI)'
    case 'capacity':
      return 'Capacity (CN/CI)'
    case 'mixed':
      return 'Fiber and wavelength'
    default:
      return 'Cable'
  }
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

function placeEdges(edges, byID) {
  return edges
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
            deviceASite: a.site,
            deviceBSite: b.site,
          }
        : null
    })
    .filter((e) => e !== null)
}

// Several services between the same two devices share one screen line.
// Bow each successive one a little higher so the colors stay visible.
function bowServiceLinks(links) {
  const seen = new Map()
  return links.map((e) => {
    const key =
      e.device_a_id < e.device_b_id
        ? `${e.device_a_id}:${e.device_b_id}`
        : `${e.device_b_id}:${e.device_a_id}`
    const n = seen.get(key) ?? 0
    seen.set(key, n + 1)
    return { ...e, bow: 0.22 + n * 0.14 }
  })
}

function buildLayers(devices, edges, serviceLinks, sites) {
  const palette = overlayPalette()
  const byID = new Map(devices.map((d) => [d.id, d]))

  const arcs = placeEdges(edges, byID)
  const serviceArcs = bowServiceLinks(placeEdges(serviceLinks, byID))
  const labeledArcs = [...arcs, ...serviceArcs].filter((e) => e.label)

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
    // Wide, nearly-invisible pick target: the painted arc is 1.5–2px, which
    // is too thin to rest a pointer on. The visual layer drawn on top is
    // not pickable so it doesn't steal hits from this one.
    new ArcLayer({
      id: 'connections-hit',
      data: arcs,
      pickable: true,
      getSourcePosition: (d) => d.source,
      getTargetPosition: (d) => d.target,
      getSourceColor: [0, 0, 0, 1],
      getTargetColor: [0, 0, 0, 1],
      getWidth: CONNECTION_HIT_WIDTH_PX,
      widthMinPixels: CONNECTION_HIT_WIDTH_PX,
      getHeight: 0,
      greatCircle: true,
      onHover: (info) => handleHover('edge', info),
    }),
    new ArcLayer({
      id: 'connections',
      data: arcs,
      pickable: false,
      getSourcePosition: (d) => d.source,
      getTargetPosition: (d) => d.target,
      getSourceColor: (d) => linkColor(d.kind),
      getTargetColor: (d) => linkColor(d.kind),
      getWidth: palette.arcWidth,
      getHeight: 0,
      greatCircle: true,
    }),
    new ArcLayer({
      id: 'service-links-hit',
      data: serviceArcs,
      pickable: true,
      getSourcePosition: (d) => d.source,
      getTargetPosition: (d) => d.target,
      getSourceColor: [0, 0, 0, 1],
      getTargetColor: [0, 0, 0, 1],
      getWidth: CONNECTION_HIT_WIDTH_PX,
      widthMinPixels: CONNECTION_HIT_WIDTH_PX,
      getHeight: (d) => d.bow,
      greatCircle: true,
      onHover: (info) => handleHover('edge', info),
    }),
    new ArcLayer({
      id: 'service-links',
      data: serviceArcs,
      pickable: false,
      getSourcePosition: (d) => d.source,
      getTargetPosition: (d) => d.target,
      getSourceColor: (d) => linkColor(d.kind),
      getTargetColor: (d) => linkColor(d.kind),
      getWidth: palette.arcWidth + 0.5,
      getHeight: (d) => d.bow,
      greatCircle: true,
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
      pickable: true,
      getPosition: (d) => d.midpoint,
      getText: (d) => d.label,
      getColor: (d) => (d.kind ? linkColor(d.kind) : palette.edgeLabelText),
      getSize: 11,
      background: true,
      getBackgroundColor: palette.labelBg,
      backgroundPadding: [4, 2],
      fontFamily: '"Helvetica Neue", Arial, sans-serif',
      onHover: (info) => handleHover('edge', info),
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
    if (d.latitude == null || d.longitude == null) return false
    if (!activeRoles.value.has(d.role || 'Unassigned')) return false
    if (opticalOnly.value && !d.optical_kind) return false
    return true
  })
  const laidOutDevices = layoutDevices(devices)
  const laidOutSites = layoutSites(
    rawSites.value.filter((s) => s.latitude != null && s.longitude != null),
  )
  overlay?.setProps({
    layers: buildLayers(laidOutDevices, rawEdges.value, rawServiceLinks.value, laidOutSites),
  })
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
      rawServiceLinks.value = data.service_links ?? []
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

function applyAllDevices(data) {
  allDevices.value = data.devices ?? []
  allSites.value = data.sites ?? []
  if (assignSelected.value) {
    assignSelected.value =
      allDevices.value.find((d) => d.id === assignSelected.value.id) ?? assignSelected.value
  }
  if (assignSelectedSite.value) {
    assignSelectedSite.value =
      allSites.value.find((s) => s.id === assignSelectedSite.value.id) ?? assignSelectedSite.value
  }
}

function loadAllDevices() {
  allDevicesLoading.value = true
  return getTopologyDevices()
    .then((data) => {
      applyAllDevices(data)
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
  assignSelectedSite.value = null
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

function selectAssignSite(site) {
  assignSelectedSite.value = site
  assignSelected.value = null
  selected.value = null
  clearPickedAddress()
  pickedCoords.value = hasMappableCoords(site) ? { lat: site.latitude, lng: site.longitude } : null
  if (hasMappableCoords(site)) {
    panTo(site.latitude, site.longitude)
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
    assignSelectedSite.value = null
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

function sameSite(d, siteName, siteId) {
  if (siteId && d.site_id && d.site_id === siteId) return true
  if (siteName && d.site === siteName) return true
  return false
}

function coordsMatch(d, lat, lng) {
  return d.latitude === lat && d.longitude === lng
}

// Map layers read TopologyDeviceDTO (plain lat/lng). The assign panel uses
// the list DTO, where GPS is optional — copy the assigned numbers onto the
// shape rebuild()/layoutDevices expect.
function deviceOnMap(d, latitude, longitude) {
  return {
    id: d.id,
    name: d.name,
    site: d.site,
    role: d.role || 'Unassigned',
    status: d.status,
    manufacturer: d.manufacturer,
    model_name: d.model_name,
    optical_kind: d.optical_kind ?? '',
    latitude: Number(latitude),
    longitude: Number(longitude),
  }
}

function revealRole(role) {
  const r = role || 'Unassigned'
  if (activeRoles.value.has(r)) return
  activeRoles.value = new Set([...activeRoles.value, r])
}

// Insert or move devices on the map immediately after an assign. A device
// that had no GPS is missing from rawDevices (the topology payload omits
// it), so mapping over the existing array would never add it.
function placeDevicesOnMap(devices, latitude, longitude) {
  if (latitude == null || longitude == null || !devices.length) return
  const byId = new Map(rawDevices.value.map((d) => [d.id, d]))
  for (const d of devices) {
    const next = deviceOnMap(d, latitude, longitude)
    byId.set(next.id, { ...byId.get(next.id), ...next })
    revealRole(next.role)
  }
  rawDevices.value = [...byId.values()]
}

function showAssignedOnMap(latitude, longitude) {
  pickedCoords.value = null
  setPicking(false)
  rebuild()
  panTo(latitude, longitude)
}

// AssignLocation only returns the pinned device. Other devices at that
// site inherit the site's GPS (see netbox.AssignDeviceLocation), so
// refresh them in the assign panel and on the map instead of leaving
// them stacked on the old point. Devices with their own distinct GPS
// are left alone — same rule as the server.
function refreshDevicesAtSite(assigned, site, latitude, longitude) {
  if (assigned) {
    assignSelected.value = { ...assigned, latitude, longitude }
  }
  // No site in the response means GPS was written on this device only —
  // leave every other device (including ones that share its current
  // site) where they are.
  if (!site) {
    if (!assigned) return
    allDevices.value = allDevices.value.map((d) =>
      d.id === assigned.id ? { ...d, ...assigned, latitude, longitude } : d,
    )
    placeDevicesOnMap([{ ...assigned, latitude, longitude }], latitude, longitude)
    return
  }

  const siteName = site.name
  const siteId = assigned?.site_id ?? site.id

  const prev = rawSites.value.find(
    (s) => (site?.id != null && s.id === site.id) || (siteName && s.name === siteName),
  )
  const inheritsSite = (d) => {
    if (d.latitude == null && d.longitude == null) return true
    return !!(prev && coordsMatch(d, prev.latitude, prev.longitude))
  }

  const moved = []
  allDevices.value = allDevices.value.map((d) => {
    if (assigned && d.id === assigned.id) {
      const next = {
        ...d,
        ...assigned,
        site: siteName || d.site,
        site_id: siteId || d.site_id,
        latitude,
        longitude,
      }
      moved.push(next)
      return next
    }
    if (!sameSite(d, siteName, siteId) || !inheritsSite(d)) return d
    const next = {
      ...d,
      site: siteName || d.site,
      site_id: siteId || d.site_id,
      latitude,
      longitude,
    }
    moved.push(next)
    return next
  })
  if (assigned && !moved.some((d) => d.id === assigned.id)) {
    moved.push({ ...assigned, site: siteName, site_id: siteId, latitude, longitude })
  }
  placeDevicesOnMap(moved, latitude, longitude)
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
      const viaNetbox = !!device.netbox_id
      toast.add({
        color: 'success',
        title: 'Assigned',
        description: site_name
          ? `${device.name} → ${data.site?.name ?? site_name}${viaNetbox ? ' in NetBox' : ''}.`
          : `Coordinates saved on ${device.name}${viaNetbox ? ' in NetBox' : ''}.`,
        duration: 4000,
      })
      refreshDevicesAtSite(data.device ?? device, data.site, latitude, longitude)
      upsertRawSite(data.site)
      showAssignedOnMap(latitude, longitude)
      return reloadAfterAssign(latitude, longitude)
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

function upsertRawSite(site) {
  if (!site?.id) return
  const rest = rawSites.value.filter((s) => s.id !== site.id && s.name !== site.name)
  rawSites.value = [...rest, site]
}

function reloadAfterAssign(latitude, longitude) {
  return Promise.all([
    getTopology().then((topo) => {
      rawDevices.value = topo.devices ?? []
      rawEdges.value = topo.edges ?? []
      rawServiceLinks.value = topo.service_links ?? []
      rawSites.value = topo.sites ?? []
      rebuild()
      panTo(latitude, longitude)
    }),
    getTopologyDevices().then((list) => {
      applyAllDevices(list)
    }),
  ])
}

function onAssignSite({ latitude, longitude, via_device_id, physical_address }) {
  const site = assignSelectedSite.value
  if (!site) return
  if (via_device_id) {
    const device = allDevices.value.find((d) => d.id === Number(via_device_id))
    if (!device) return
    assignSaving.value = true
    const body = { latitude, longitude, site_name: site.name }
    if (physical_address) body.physical_address = physical_address
    assignDeviceLocation(device.id, body)
      .then((data) => {
        toast.add({
          color: 'success',
          title: 'Assigned',
          description: `${data.site?.name ?? site.name} updated in NetBox via ${device.name}.`,
          duration: 4000,
        })
        refreshDevicesAtSite(data.device ?? device, data.site ?? site, latitude, longitude)
        upsertRawSite(data.site)
        if (data.site) {
          assignSelectedSite.value = {
            ...site,
            ...data.site,
            latitude: data.site.latitude,
            longitude: data.site.longitude,
          }
        }
        showAssignedOnMap(latitude, longitude)
        return reloadAfterAssign(latitude, longitude)
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
    return
  }
  assignSaving.value = true
  assignSiteLocation(site.id, { latitude, longitude })
    .then((data) => {
      toast.add({
        color: 'success',
        title: 'Assigned',
        description: `Coordinates saved on ${data.site?.name ?? site.name}.`,
        duration: 4000,
      })
      upsertRawSite(data.site)
      if (data.site) {
        assignSelectedSite.value = {
          ...site,
          ...data.site,
          latitude: data.site.latitude,
          longitude: data.site.longitude,
        }
      }
      refreshDevicesAtSite(null, data.site ?? site, latitude, longitude)
      showAssignedOnMap(latitude, longitude)
      return reloadAfterAssign(latitude, longitude)
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

watch([assignSelected, assignSelectedSite, pickedCoords], () => {
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

  overlay = new MapboxOverlay({ layers: [], pickingRadius: PICKING_RADIUS_PX })
  map.addControl(overlay)

  map.on('click', (e) => {
    if (!pickingCoords.value) return
    // NetBox GPS fields are xx.yyyyyy (6 decimal places); a raw map click
    // has more digits and NetBox rejects the site POST with 400.
    const lat = Number(e.lngLat.lat.toFixed(6))
    const lng = Number(e.lngLat.lng.toFixed(6))
    pickedCoords.value = { lat, lng }
    lookupPickedAddress(lat, lng)
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
  <div class="card flex min-h-0 flex-1 flex-col overflow-hidden">
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
        <span class="mx-1 h-3 w-px bg-default" />
        <span v-for="item in LINK_LEGEND" :key="item.kind" class="flex items-center gap-1">
          <span
            class="inline-block h-0.5 w-4 rounded-full"
            :style="{ background: linkSwatch(item.kind) }"
          />
          {{ item.label }}
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

    <div class="flex min-h-0 flex-1 gap-3 overflow-hidden">
      <SiteAssignPanel
        v-if="assignMode"
        :devices="allDevices"
        :sites="allSites.length ? allSites : rawSites"
        :selected-id="assignSelected?.id ?? null"
        :selected-site-id="assignSelectedSite?.id ?? null"
        :can-write="authStore.canWrite"
        :picking="pickingCoords"
        :picked="pickedCoords"
        :address="pickedAddress"
        :address-loading="pickedAddressLoading"
        :saving="assignSaving"
        :loading="allDevicesLoading"
        @select="selectAssignDevice"
        @select-site="selectAssignSite"
        @update:picking="setPicking"
        @use-site="onUseSite"
        @assign="onAssign"
        @assign-site="onAssignSite"
      />
      <div
        ref="mapWrap"
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
            <div v-if="hardwareLabel(selected)">{{ hardwareLabel(selected) }}</div>
            <div>Site: {{ selected.site || '-' }}</div>
            <div>Role: {{ selected.role || '-' }}</div>
            <div>Status: {{ selected.status || '-' }}</div>
          </div>
        </div>

        <div
          v-if="hoverInfo"
          class="absolute z-20 w-max max-w-80 rounded-md border border-default bg-default px-2.5 py-1.5 text-xs shadow-lg pointer-events-none"
          :style="hoverPos"
        >
          <template v-if="hoverInfo.kind === 'device'">
            <div class="font-medium">{{ hoverInfo.object.name }}</div>
            <div v-if="hardwareLabel(hoverInfo.object)" class="text-muted-color">
              {{ hardwareLabel(hoverInfo.object) }}
            </div>
            <div class="text-muted-color">
              {{ hoverInfo.object.site || '-' }} · {{ hoverInfo.object.role || '-' }} ·
              {{ hoverInfo.object.status || '-' }}
            </div>
          </template>
          <template v-else-if="hoverInfo.kind === 'site'">
            <div class="font-medium">{{ hoverInfo.object.name }}</div>
          </template>
          <template v-else>
            <div class="space-y-1.5">
              <div>
                <div class="font-medium">{{ kindLabel(hoverInfo.object.kind) }}</div>
                <div v-if="hoverInfo.object.service_id" class="text-muted-color">
                  {{ hoverInfo.object.service_id }}
                </div>
                <div v-else-if="hoverInfo.object.service_ids?.length" class="text-muted-color">
                  {{ hoverInfo.object.service_ids.join(', ') }}
                </div>
              </div>
              <div>
                <div class="text-[10px] font-semibold uppercase tracking-wide text-muted-color">
                  A
                </div>
                <div class="font-medium">{{ hoverInfo.object.deviceAName }}</div>
                <div class="text-muted-color">
                  {{ siteLabel(hoverInfo.object.deviceASite) }}
                  <template v-if="hoverInfo.object.interface_a">
                    · {{ hoverInfo.object.interface_a }}
                  </template>
                </div>
                <div
                  v-if="hoverInfo.object.interface_a_description"
                  class="text-muted-color whitespace-pre-wrap"
                >
                  {{ hoverInfo.object.interface_a_description }}
                </div>
              </div>
              <div>
                <div class="text-[10px] font-semibold uppercase tracking-wide text-muted-color">
                  B
                </div>
                <div class="font-medium">{{ hoverInfo.object.deviceBName }}</div>
                <div class="text-muted-color">
                  {{ siteLabel(hoverInfo.object.deviceBSite) }}
                  <template v-if="hoverInfo.object.interface_b">
                    · {{ hoverInfo.object.interface_b }}
                  </template>
                </div>
                <div
                  v-if="hoverInfo.object.interface_b_description"
                  class="text-muted-color whitespace-pre-wrap"
                >
                  {{ hoverInfo.object.interface_b_description }}
                </div>
              </div>
              <div v-if="hoverInfo.object.label" class="text-muted-color">
                {{ hoverInfo.object.label }}
              </div>
            </div>
          </template>
        </div>
      </div>
    </div>
  </div>
</template>
