// Overview vs street-level drawing for the network map.
// Below SITE_DETAIL_ZOOM, devices that share a real site become one marker
// named for that site. At street level the map draws every device again.
// The margin keeps a zoom gesture from flipping the mode on every wheel tick
// while the level sits on the boundary.

export const SITE_DETAIL_ZOOM = 14
const CLUSTER_HYSTERESIS = 0.35
// Comfortably past the exit threshold, so a click or a "show this device"
// pan actually leaves the site marker and draws the devices.
export const SITE_OPEN_ZOOM = 15

export function clusterModeForZoom(clustered, zoom) {
  if (clustered) return zoom < SITE_DETAIL_ZOOM + CLUSTER_HYSTERESIS
  return zoom < SITE_DETAIL_ZOOM - CLUSTER_HYSTERESIS
}

export function isPlaceholderSite(name) {
  const s = String(name ?? '').trim()
  return s === '' || s.toLowerCase() === 'default'
}

const STATUS_RANK = {
  offline: 4,
  failed: 4,
  decommissioning: 4,
  planned: 3,
  staged: 3,
  active: 2,
}

function finite(v) {
  const n = Number(v)
  return Number.isFinite(n) ? n : null
}

export function aggregateStatus(devices) {
  let best = ''
  let rank = -1
  for (const d of devices) {
    const s = String(d.status ?? '')
      .trim()
      .toLowerCase()
    const r = Object.hasOwn(STATUS_RANK, s) ? STATUS_RANK[s] : s ? 1 : 0
    if (r > rank) {
      rank = r
      best = s
    }
  }
  return best
}

function statusText(devices) {
  const counts = new Map()
  for (const d of devices) {
    const s =
      String(d.status ?? '')
        .trim()
        .toLowerCase() || 'unknown'
    counts.set(s, (counts.get(s) ?? 0) + 1)
  }
  return [...counts.entries()].map(([s, n]) => `${n} ${s}`).join(', ')
}

function anchorFor(name, members, sites) {
  const site = sites.find((s) => {
    return (
      String(s.name ?? '').trim() === name &&
      finite(s.latitude) != null &&
      finite(s.longitude) != null
    )
  })
  if (site) return { latitude: finite(site.latitude), longitude: finite(site.longitude) }
  let lat = 0
  let lng = 0
  let n = 0
  for (const d of members) {
    const la = finite(d.latitude)
    const ln = finite(d.longitude)
    if (la == null || ln == null) continue
    lat += la
    lng += ln
    n += 1
  }
  if (!n) return { latitude: 0, longitude: 0 }
  return { latitude: lat / n, longitude: lng / n }
}

function posOf(node) {
  if (node.cluster) return [node.cluster.mapLng, node.cluster.mapLat]
  const d = node.device
  return [finite(d.mapLng) ?? finite(d.longitude), finite(d.mapLat) ?? finite(d.latitude)]
}

function mergeCableKind(kinds) {
  const set = new Set(kinds.map((k) => (k ? k : 'cable')))
  if (set.size === 1) {
    const only = [...set][0]
    return only === 'cable' ? '' : only
  }
  if (set.has('mixed') || (set.has('fiber') && set.has('wavelength'))) return 'mixed'
  if (set.has('fiber')) return 'fiber'
  if (set.has('wavelength')) return 'wavelength'
  if (set.has('capacity')) return 'capacity'
  return 'mixed'
}

function midpoint(source, target) {
  return [(source[0] + target[0]) / 2, (source[1] + target[1]) / 2]
}

// Group visible devices by site and collapse links that share both ends.
// Individuals are devices with no real site; they stay separate markers.
// Arcs are already positioned. Intra-site links are dropped. Parallel
// cables between the same pair become one arc (service links stay split
// by kind). `label` is cleared so overview zoom does not stamp every
// cable name on the same midpoint.
export function aggregateBySite({ devices, sites, edges, serviceLinks }) {
  const siteList = sites ?? []
  const members = new Map()
  const individuals = []
  for (const d of devices ?? []) {
    if (isPlaceholderSite(d.site)) {
      individuals.push(d)
      continue
    }
    const key = String(d.site).trim()
    if (!members.has(key)) members.set(key, [])
    members.get(key).push(d)
  }

  const clusters = []
  const deviceNode = new Map()
  for (const [name, group] of members) {
    const { latitude, longitude } = anchorFor(name, group, siteList)
    const cluster = {
      id: `site:${name}`,
      name,
      site: name,
      latitude,
      longitude,
      mapLat: latitude,
      mapLng: longitude,
      deviceCount: group.length,
      devices: group,
      status: aggregateStatus(group),
      statusText: statusText(group),
    }
    clusters.push(cluster)
    for (const d of group) deviceNode.set(d.id, { cluster })
  }
  for (const d of individuals) deviceNode.set(d.id, { device: d })

  const clusteredNames = new Set(clusters.map((c) => c.name))
  const bareSites = siteList.filter((s) => !clusteredNames.has(String(s.name ?? '').trim()))

  function collapse(list, { byKind }) {
    const groups = new Map()
    for (const e of list ?? []) {
      const a = deviceNode.get(e.device_a_id)
      const b = deviceNode.get(e.device_b_id)
      if (!a || !b) continue
      const aKey = a.cluster ? a.cluster.id : `device:${a.device.id}`
      const bKey = b.cluster ? b.cluster.id : `device:${b.device.id}`
      if (aKey === bKey) continue
      const [lo, hi, left, right] = aKey < bKey ? [aKey, bKey, a, b] : [bKey, aKey, b, a]
      const kindKey = byKind ? e.kind || '' : '*'
      const key = `${lo}|${hi}|${kindKey}`
      let g = groups.get(key)
      if (!g) {
        g = { left, right, kinds: [], count: 0, sample: e }
        groups.set(key, g)
      }
      g.kinds.push(e.kind || '')
      g.count += 1
    }
    return [...groups.values()].map((g) => {
      const source = posOf(g.left)
      const target = posOf(g.right)
      const kind = byKind ? g.sample.kind || '' : mergeCableKind(g.kinds)
      const single = g.count === 1
      return {
        ...g.sample,
        kind,
        label: '',
        linkCount: g.count,
        source,
        target,
        midpoint: midpoint(source, target),
        sourceDeviceId: g.left.device?.id ?? null,
        targetDeviceId: g.right.device?.id ?? null,
        deviceAName: g.left.cluster ? g.left.cluster.name : g.left.device.name,
        deviceBName: g.right.cluster ? g.right.cluster.name : g.right.device.name,
        deviceASite: g.left.cluster ? g.left.cluster.name : g.left.device.site,
        deviceBSite: g.right.cluster ? g.right.cluster.name : g.right.device.site,
        interface_a: single ? g.sample.interface_a : '',
        interface_b: single ? g.sample.interface_b : '',
        interface_a_description: single ? g.sample.interface_a_description : '',
        interface_b_description: single ? g.sample.interface_b_description : '',
        service_id: single ? g.sample.service_id : '',
        service_ids: single ? g.sample.service_ids : undefined,
      }
    })
  }

  return {
    clusters,
    individuals,
    bareSites,
    arcs: collapse(edges, { byKind: false }),
    serviceArcs: collapse(serviceLinks, { byKind: true }),
  }
}

// Ring layout moves unsited devices off the shared point. Shift only the
// ends that still belong to a device; a site anchor stays put.
export function applyLaidOutEndpoints(arcs, laidOut) {
  const byId = new Map((laidOut ?? []).map((d) => [d.id, d]))
  return (arcs ?? []).map((e) => {
    const a = e.sourceDeviceId != null ? byId.get(e.sourceDeviceId) : null
    const b = e.targetDeviceId != null ? byId.get(e.targetDeviceId) : null
    if (!a && !b) return e
    const source = a ? [a.mapLng, a.mapLat] : e.source
    const target = b ? [b.mapLng, b.mapLat] : e.target
    return { ...e, source, target, midpoint: midpoint(source, target) }
  })
}
