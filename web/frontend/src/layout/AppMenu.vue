<script setup>
import { computed } from 'vue'
import { useAuthStore } from '@/stores/auth'

const authStore = useAuthStore()

const items = computed(() => {
  const groups = [
    [
      { type: 'label', label: 'Home' },
      { label: 'Dashboard', icon: 'i-lucide-home', to: '/', exact: true },
    ],
  ]

  // A user with no role (not admin/operator/viewer) only gets the dashboard
  // and their own profile - see web/auth.go's RequireRead/RequireWrite,
  // which reject every other API route for them.
  if (authStore.canRead) {
    groups.push(
      ...(authStore.organizationEnabled
        ? [
            [
              { type: 'label', label: 'Organization' },
              { label: 'Customers', icon: 'i-lucide-building-2', to: '/tenant/customer' },
              { label: 'Contacts', icon: 'i-lucide-book-user', to: '/tenant/contact' },
            ],
          ]
        : []),
      [
        { type: 'label', label: 'Devices' },
        { label: 'Network map', icon: 'i-lucide-globe', to: '/network-map' },
        { label: 'Devices', icon: 'i-lucide-server', to: '/device' },
        ...(authStore.oxidizedEnabled
          ? [{ label: 'Oxidized', icon: 'i-lucide-save', to: '/oxidized' }]
          : []),
      ],
      ...(authStore.ipamEnabled
        ? [
            [
              { type: 'label', label: 'IPAM' },
              { label: 'Prefixes', icon: 'i-lucide-binary', to: '/ipam' },
            ],
          ]
        : []),
      [
        { type: 'label', label: 'Provisioning' },
        { label: 'Services', icon: 'i-lucide-zap', to: '/service' },
        { label: 'Config', icon: 'i-lucide-settings-2', to: '/config' },
        ...(authStore.opticalEnabled
          ? [{ label: 'Maintenance', icon: 'i-lucide-wrench', to: '/maintenance' }]
          : []),
      ],
      [
        { type: 'label', label: 'Jobs' },
        { label: 'Job overview', icon: 'i-lucide-refresh-cw', to: '/sync/overview' },
        { label: 'Job status', icon: 'i-lucide-list-checks', to: '/sync/status' },
        { label: 'Scheduler', icon: 'i-lucide-clock', to: '/sync/schedules' },
        {
          label: 'Device deletions',
          icon: 'i-lucide-trash-2',
          to: '/sync/librenms-deletions',
        },
      ],
    )
  }

  groups.push([
    { type: 'label', label: 'Help' },
    { label: 'Documentation', icon: 'i-lucide-book-open', to: '/doc' },
  ])

  if (authStore.isAdmin) {
    groups.push([
      { type: 'label', label: 'Admin' },
      {
        label: 'Settings',
        icon: 'i-lucide-settings',
        children: [
          { label: 'Factum', icon: 'i-lucide-sliders-horizontal', to: '/admin/settings/factum' },
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
    ])
  }

  return groups
})
</script>

<template>
  <UNavigationMenu orientation="vertical" :items="items" class="w-full" />
</template>
