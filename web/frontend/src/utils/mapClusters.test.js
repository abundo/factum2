import assert from 'node:assert/strict'
import { test } from 'node:test'
import {
  SITE_DETAIL_ZOOM,
  aggregateBySite,
  aggregateStatus,
  applyLaidOutEndpoints,
  clusterModeForZoom,
  isPlaceholderSite,
} from './mapClusters.js'

const site = (name, latitude, longitude) => ({ name, latitude, longitude })
const dev = (id, name, siteName, latitude, longitude, status = 'active') => ({
  id,
  name,
  site: siteName,
  latitude,
  longitude,
  status,
})

test('placeholder sites are Default and blank', () => {
  assert.equal(isPlaceholderSite(''), true)
  assert.equal(isPlaceholderSite('  '), true)
  assert.equal(isPlaceholderSite('Default'), true)
  assert.equal(isPlaceholderSite('default'), true)
  assert.equal(isPlaceholderSite('Ankarskatan'), false)
})

test('cluster mode stays put across the zoom boundary', () => {
  assert.equal(clusterModeForZoom(true, SITE_DETAIL_ZOOM), true)
  assert.equal(clusterModeForZoom(true, SITE_DETAIL_ZOOM + 0.2), true)
  assert.equal(clusterModeForZoom(true, SITE_DETAIL_ZOOM + 0.35), false)
  assert.equal(clusterModeForZoom(false, SITE_DETAIL_ZOOM), false)
  assert.equal(clusterModeForZoom(false, SITE_DETAIL_ZOOM - 0.2), false)
  assert.equal(clusterModeForZoom(false, SITE_DETAIL_ZOOM - 0.36), true)
})

test('offline outranks active when a site is one marker', () => {
  assert.equal(aggregateStatus([{ status: 'active' }, { status: 'Offline' }]), 'offline')
  assert.equal(aggregateStatus([{ status: 'planned' }, { status: 'active' }]), 'planned')
})

test('devices that share a site become one marker at the site coordinate', () => {
  const devices = [
    dev(1, 'au4-ankarskatan', 'Ankarskatan', 65.6, 22.1),
    dev(2, 'au5-ankarskatan', 'Ankarskatan', 65.61, 22.12),
    dev(3, 'dr-28', 'Faron', 65.7, 22.3),
    dev(4, 'lone', 'Default', 65.5, 22.0),
    dev(5, 'other', '', 65.4, 21.9),
  ]
  const sites = [site('Ankarskatan', 65.59, 22.11), site('Empty', 65.1, 21.5)]
  const edges = [
    { device_a_id: 1, device_b_id: 2, kind: '', label: 'lldp' },
    { device_a_id: 1, device_b_id: 3, kind: 'fiber', label: 'lldp' },
    { device_a_id: 2, device_b_id: 3, kind: '', label: 'lldp' },
    { device_a_id: 4, device_b_id: 3, kind: 'wavelength', label: 'lldp' },
  ]
  const overview = aggregateBySite({
    devices,
    sites,
    edges,
    serviceLinks: [
      { device_a_id: 1, device_b_id: 3, kind: 'capacity', service_id: 'CN1', label: 'CN1' },
      { device_a_id: 2, device_b_id: 3, kind: 'capacity', service_id: 'CN2', label: 'CN2' },
      { device_a_id: 1, device_b_id: 3, kind: 'fiber', service_id: 'LF1', label: 'LF1' },
    ],
  })

  assert.deepEqual(
    overview.clusters.map((c) => c.name),
    ['Ankarskatan', 'Faron'],
  )
  const ankar = overview.clusters[0]
  assert.equal(ankar.deviceCount, 2)
  assert.equal(ankar.latitude, 65.59)
  assert.equal(ankar.longitude, 22.11)
  assert.deepEqual(
    overview.individuals.map((d) => d.name),
    ['lone', 'other'],
  )
  assert.deepEqual(
    overview.bareSites.map((s) => s.name),
    ['Empty'],
  )

  assert.equal(overview.arcs.length, 2)
  const between = overview.arcs.find((e) => e.linkCount === 2)
  assert.equal(between.kind, 'fiber')
  assert.equal(between.label, '')
  assert.deepEqual(between.source, [22.11, 65.59])
  assert.equal(between.deviceAName, 'Ankarskatan')
  assert.equal(between.deviceBName, 'Faron')
  assert.equal(between.interface_a, '')

  const toLone = overview.arcs.find((e) => e.linkCount === 1)
  assert.equal(toLone.kind, 'wavelength')
  assert.equal(toLone.deviceAName, 'lone')
  assert.equal(toLone.deviceBName, 'Faron')

  assert.equal(overview.serviceArcs.length, 2)
  const capacity = overview.serviceArcs.find((e) => e.kind === 'capacity')
  assert.equal(capacity.linkCount, 2)
  assert.equal(capacity.label, '')
  assert.equal(capacity.service_id, '')
  assert.equal(overview.serviceArcs.find((e) => e.kind === 'fiber').linkCount, 1)
})

test('a site with no coordinates uses the device centroid', () => {
  const overview = aggregateBySite({
    devices: [dev(1, 'a', 'Pop', 10, 20), dev(2, 'b', 'Pop', 12, 24)],
    sites: [],
    edges: [],
    serviceLinks: [],
  })
  assert.equal(overview.clusters.length, 1)
  assert.equal(overview.clusters[0].latitude, 11)
  assert.equal(overview.clusters[0].longitude, 22)
})

test('fiber and wavelength on the same site pair are mixed', () => {
  const overview = aggregateBySite({
    devices: [dev(1, 'a', 'A', 1, 1), dev(2, 'b', 'B', 2, 2)],
    sites: [],
    edges: [
      { device_a_id: 1, device_b_id: 2, kind: 'fiber' },
      { device_a_id: 1, device_b_id: 2, kind: 'wavelength' },
    ],
    serviceLinks: [],
  })
  assert.equal(overview.arcs.length, 1)
  assert.equal(overview.arcs[0].kind, 'mixed')
})

test('ring layout moves only the unsited end of an overview arc', () => {
  const overview = aggregateBySite({
    devices: [dev(1, 'lone', '', 10, 20), dev(2, 'core', 'Pop', 11, 21)],
    sites: [site('Pop', 11, 21)],
    edges: [{ device_a_id: 1, device_b_id: 2, kind: '' }],
    serviceLinks: [],
  })
  const [arc] = applyLaidOutEndpoints(overview.arcs, [{ id: 1, mapLng: 20.01, mapLat: 10.01 }])
  assert.deepEqual(arc.source, [20.01, 10.01])
  assert.deepEqual(arc.target, [21, 11])
  assert.deepEqual(arc.midpoint, [(20.01 + 21) / 2, (10.01 + 11) / 2])
})
