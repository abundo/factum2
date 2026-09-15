<script setup>
import { computed, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { useAuthStore } from '@/stores/auth'

const authStore = useAuthStore()
const route = useRoute()

// Shared across the per-group AccordionRoots inside UNavigationMenu. Selecting a
// leaf item (or changing route) keeps only the matching heading(s) open.
const openSections = ref([])

function section(label, children) {
  return [
    {
      label,
      value: label.toLowerCase(),
      children,
    },
  ]
}

function pathMatches(to, exact, path) {
  if (!to) return false
  if (exact) return path === to
  return path === to || path.startsWith(`${to}/`)
}

function containsPath(item, path) {
  if (pathMatches(item.to, item.exact, path)) return true
  return item.children?.some((child) => containsPath(child, path)) ?? false
}

function openValuesForPath(path) {
  const values = []
  for (const group of groups.value) {
    for (const item of group) {
      if (item.value && containsPath(item, path)) {
        values.push(item.value)
      }
    }
  }
  return values
}

function collapseToPath(path) {
  openSections.value = openValuesForPath(path)
}

function topLevelValues() {
  const values = new Set()
  for (const group of groups.value) {
    for (const item of group) {
      if (item.value) values.add(item.value)
    }
  }
  return values
}

// Nested accordions (Admin > Settings/AAA) also emit update:modelValue through
// UNavigationMenu. Ignore those so they don't collapse every top-level heading.
function onOpenSectionsUpdate(val) {
  const incoming = Array.isArray(val) ? val : val != null ? [val] : []
  const allowed = topLevelValues()
  const next = incoming.filter((v) => allowed.has(v))
  if (next.length > 0 || incoming.length === 0) {
    openSections.value = next
  }
}

function decorate(item, path) {
  const next = { ...item }
  if (next.children?.length) {
    next.children = next.children.map((child) => decorate(child, path))
    next.open = containsPath(next, path)
  } else if (next.to) {
    next.onSelect = () => collapseToPath(next.to)
  }
  return next
}

const groups = computed(() => {
  const result = [
    section('Home', [{ label: 'Dashboard', icon: 'i-lucide-home', to: '/', exact: true }]),
  ]

  // A user with no role (not admin/operator/viewer) only gets the dashboard
  // and their own profile - see web/auth.go's RequireRead/RequireWrite,
  // which reject every other API route for them.
  if (authStore.canRead) {
    result.push(
      ...(authStore.organizationEnabled
        ? [
            section('Organization', [
              { label: 'Customers', icon: 'i-lucide-building-2', to: '/tenant/customer' },
              { label: 'Contacts', icon: 'i-lucide-book-user', to: '/tenant/contact' },
              { label: 'Sites', icon: 'i-lucide-map-pin', to: '/tenant/site' },
            ]),
          ]
        : []),
      section('DCIM', [
        { label: 'Network map', icon: 'i-lucide-globe', to: '/network-map' },
        { label: 'Devices', icon: 'i-lucide-server', to: '/device' },
        { label: 'Interfaces', icon: 'i-lucide-cable', to: '/dcim/interfaces' },
        { label: 'Manufacturers', icon: 'i-lucide-factory', to: '/dcim/manufacturers' },
        { label: 'Device types', icon: 'i-lucide-cpu', to: '/dcim/device-types' },
        { label: 'Platforms', icon: 'i-lucide-layers', to: '/dcim/platforms' },
        ...(authStore.oxidizedEnabled
          ? [{ label: 'Oxidized', icon: 'i-lucide-save', to: '/oxidized' }]
          : []),
      ]),
      ...(authStore.ipamEnabled
        ? [section('IPAM', [{ label: 'Prefixes', icon: 'i-lucide-binary', to: '/ipam' }])]
        : []),
      ...(authStore.dnsZonesEnabled
        ? [
            section('DNS', [
              { label: 'Zones', icon: 'i-lucide-globe-2', to: '/dns/zones' },
              { label: 'DNS templates', icon: 'i-lucide-layers', to: '/dns/templates' },
              { label: 'SOA templates', icon: 'i-lucide-file-text', to: '/dns/soa-templates' },
              { label: 'DNSSEC policies', icon: 'i-lucide-shield', to: '/dns/dnssec-policies' },
            ]),
          ]
        : []),
      section('Provisioning', [
        { label: 'Services', icon: 'i-lucide-zap', to: '/service' },
        { label: 'Config', icon: 'i-lucide-settings-2', to: '/config' },
        ...(authStore.opticalEnabled
          ? [{ label: 'Maintenance', icon: 'i-lucide-wrench', to: '/maintenance' }]
          : []),
      ]),
      section('Jobs', [
        { label: 'Job overview', icon: 'i-lucide-refresh-cw', to: '/sync/overview' },
        { label: 'Job status', icon: 'i-lucide-list-checks', to: '/sync/status' },
        { label: 'Scheduler', icon: 'i-lucide-clock', to: '/sync/schedules' },
        {
          label: 'Device deletions',
          icon: 'i-lucide-trash-2',
          to: '/sync/librenms-deletions',
        },
      ]),
    )
  }

  result.push(section('Help', [{ label: 'Documentation', icon: 'i-lucide-book-open', to: '/doc' }]))

  if (authStore.isAdmin) {
    result.push(
      section('Admin', [
        {
          label: 'Settings',
          icon: 'i-lucide-settings',
          children: [
            {
              label: 'Factum',
              icon: 'i-lucide-sliders-horizontal',
              to: '/admin/settings/factum',
            },
            ...(authStore.opticalEnabled
              ? [{ label: 'Optical', icon: 'i-lucide-aperture', to: '/admin/settings/optical' }]
              : []),
            { label: 'Sources', icon: 'i-lucide-database', to: '/admin/settings/sources' },
            { label: 'Destinations', icon: 'i-lucide-send', to: '/admin/settings/destinations' },
            {
              label: 'Dashboard',
              icon: 'i-lucide-layout-dashboard',
              to: '/admin/settings/dashboard',
            },
            { label: 'Worker nodes', icon: 'i-lucide-server-cog', to: '/admin/worker-nodes' },
            { label: 'Device sync', icon: 'i-lucide-key-round', to: '/admin/device-sync' },
          ],
        },
        {
          label: 'AAA',
          icon: 'i-lucide-shield-check',
          children: [
            { label: 'Users', icon: 'i-lucide-users', to: '/admin/users' },
            { label: 'Roles', icon: 'i-lucide-shield', to: '/admin/roles' },
            { label: 'Authentication', icon: 'i-lucide-key', to: '/admin/authentication' },
            { label: 'Authorization', icon: 'i-lucide-lock', to: '/admin/authorization' },
          ],
        },
      ]),
    )
  }

  return result
})

const items = computed(() =>
  groups.value.map((group) => group.map((item) => decorate(item, route.path))),
)

watch(
  () => route.path,
  (path) => collapseToPath(path),
  { immediate: true },
)
</script>

<template>
  <UNavigationMenu
    :model-value="openSections"
    orientation="vertical"
    :items="items"
    class="w-full"
    @update:model-value="onOpenSectionsUpdate"
  />
</template>
