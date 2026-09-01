import { createRouter, createWebHistory } from 'vue-router'
import AppLayout from '@/layout/AppLayout.vue'
import { useAuthStore } from '@/stores/auth'

const router = createRouter({
  history: createWebHistory(import.meta.env.BASE_URL),
  routes: [
    {
      path: '/login',
      name: 'login',
      component: () => import('@/views/auth/LoginPage.vue'),
    },
    {
      path: '/forgot-password',
      name: 'forgot-password',
      component: () => import('@/views/auth/ForgotPasswordPage.vue'),
    },
    {
      path: '/reset-password',
      name: 'reset-password',
      component: () => import('@/views/auth/ResetPasswordPage.vue'),
    },
    {
      path: '/',
      component: AppLayout,
      children: [
        {
          path: '/',
          name: 'dashboard',
          component: () => import('@/views/DashboardPage.vue'),
        },
        {
          path: '/tenant/customer',
          name: 'customers',
          meta: { requiresRead: true, requiresOrganization: true },
          component: () => import('@/views/customer/CustomerList.vue'),
        },
        {
          path: '/tenant/contact',
          name: 'contacts',
          meta: { title: 'Contacts', requiresRead: true, requiresOrganization: true },
          component: () => import('@/views/contact/ContactList.vue'),
        },
        {
          path: '/maintenance',
          name: 'maintenance',
          meta: { requiresRead: true },
          component: () => import('@/views/maintenance/MaintenanceList.vue'),
        },
        {
          path: '/service',
          name: 'service',
          meta: { requiresRead: true },
          component: () => import('@/views/service/ServiceList.vue'),
        },
        {
          path: '/service/new',
          name: 'service-new',
          meta: { requiresWrite: true },
          component: () => import('@/views/service/ServiceCreateWizard.vue'),
        },
        {
          path: '/config',
          name: 'config',
          meta: { title: 'Config', requiresRead: true },
          component: () => import('@/views/config/ConfigPage.vue'),
        },
        {
          path: '/device',
          name: 'devices',
          meta: { requiresRead: true },
          component: () => import('@/views/device/DeviceList.vue'),
        },
        {
          path: '/oxidized',
          name: 'oxidized',
          meta: { title: 'Oxidized', requiresRead: true },
          component: () => import('@/views/oxidized/OxidizedBrowserPage.vue'),
        },
        {
          path: '/ipam',
          name: 'ipam',
          meta: { requiresRead: true, requiresIpam: true },
          component: () => import('@/views/ipam/IpamPage.vue'),
        },
        {
          path: '/ipam/:id',
          redirect: '/ipam',
        },
        {
          path: '/network-map',
          name: 'network-map',
          meta: { title: 'Network map', requiresRead: true },
          component: () => import('@/views/topology/NetworkMap.vue'),
        },
        {
          path: '/sync/overview',
          name: 'sync-overview',
          meta: { title: 'Job overview', requiresRead: true },
          component: () => import('@/views/sync/SyncOverviewPage.vue'),
        },
        {
          path: '/sync/status',
          name: 'sync-status',
          meta: { title: 'Job status', requiresRead: true },
          component: () => import('@/views/sync/JobStatusPage.vue'),
        },
        {
          path: '/sync/schedules',
          name: 'sync-schedules',
          meta: { title: 'Scheduler', requiresRead: true },
          component: () => import('@/views/sync/JobSchedulerPage.vue'),
        },
        {
          path: '/sync/librenms-deletions',
          name: 'sync-librenms-deletions',
          meta: { title: 'Device deletions', requiresRead: true },
          component: () => import('@/views/sync/LibrenmsPendingDeletesPage.vue'),
        },
        {
          path: '/report',
          name: 'reports',
          meta: { title: 'Reports' },
          component: () => import('@/views/PlaceholderPage.vue'),
        },
        {
          path: '/user-settings',
          name: 'user-settings',
          component: () => import('@/views/UserSettingsPage.vue'),
        },
        {
          path: '/admin/users',
          name: 'admin-users',
          meta: { requiresAdmin: true },
          component: () => import('@/views/admin/UserList.vue'),
        },
        {
          path: '/admin/roles',
          name: 'admin-roles',
          meta: { requiresAdmin: true },
          component: () => import('@/views/admin/RoleList.vue'),
        },
        {
          path: '/admin/worker-nodes',
          name: 'admin-worker-nodes',
          meta: { requiresAdmin: true },
          component: () => import('@/views/admin/WorkerNodeList.vue'),
        },
        {
          path: '/admin/device-sync',
          name: 'admin-device-sync',
          meta: { requiresAdmin: true },
          component: () => import('@/views/admin/DeviceSyncPage.vue'),
        },
        {
          path: '/admin/settings/sources',
          name: 'admin-settings-sources',
          meta: { requiresAdmin: true },
          component: () => import('@/views/admin/SettingsSourcesPage.vue'),
        },
        {
          path: '/admin/settings/destinations',
          name: 'admin-settings-destinations',
          meta: { requiresAdmin: true },
          component: () => import('@/views/admin/SettingsDestinationsPage.vue'),
        },
        {
          path: '/admin/settings/factum',
          name: 'admin-settings-factum',
          meta: { requiresAdmin: true },
          component: () => import('@/views/admin/SettingsFactumPage.vue'),
        },
        {
          path: '/admin/settings/optical',
          name: 'admin-settings-optical',
          meta: { requiresAdmin: true },
          component: () => import('@/views/admin/SettingsOpticalPage.vue'),
        },
        {
          path: '/admin/settings/dashboard',
          name: 'admin-settings-dashboard',
          meta: { requiresAdmin: true },
          component: () => import('@/views/admin/SettingsDashboardPage.vue'),
        },
        {
          path: '/admin/authentication',
          name: 'admin-authentication',
          meta: { requiresAdmin: true },
          component: () => import('@/views/admin/AuthenticationSettings.vue'),
        },
        {
          path: '/admin/authorization',
          name: 'admin-authorization',
          meta: { requiresAdmin: true },
          component: () => import('@/views/admin/AuthorizationSettings.vue'),
        },
        {
          path: '/doc',
          name: 'documentation',
          meta: { title: 'Documentation' },
          component: () => import('@/views/PlaceholderPage.vue'),
        },
        {
          // Alias for the old server-rendered home page.
          path: '/index.html',
          redirect: '/',
        },
      ],
    },
    {
      path: '/:pathMatch(.*)*',
      name: 'notfound',
      component: () => import('@/views/NotFound.vue'),
    },
  ],
})

const publicRouteNames = new Set(['login', 'forgot-password', 'reset-password'])

router.beforeEach((to) => {
  const authStore = useAuthStore()
  if (!publicRouteNames.has(to.name) && !authStore.isAuthenticated) {
    return { name: 'login', query: { redirect: to.fullPath } }
  }
  if (to.name === 'login' && authStore.isAuthenticated) {
    return { path: '/' }
  }

  // Mirrors the backend's role tiers (web/auth.go's RequireRead/
  // RequireWrite/RequireAdmin) - a user with no role only gets the
  // dashboard and their own profile; a viewer can look but not write; only
  // admin/operator can navigate to the "new" wizard or admin/* pages.
  if (to.meta?.requiresAdmin && !authStore.isAdmin) {
    return { path: '/' }
  }
  if (to.meta?.requiresWrite && !authStore.canWrite) {
    return { path: '/' }
  }
  if (to.meta?.requiresRead && !authStore.canRead) {
    return { path: '/' }
  }
  if (to.meta?.requiresIpam && !authStore.ipamEnabled) {
    return { path: '/' }
  }
  if (to.meta?.requiresOrganization && !authStore.organizationEnabled) {
    return { path: '/' }
  }
})

export default router
