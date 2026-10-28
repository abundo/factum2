<script setup>
import { computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import JobStatusPage from './JobStatusPage.vue'
import SyncOverviewPage from './SyncOverviewPage.vue'

defineOptions({ name: 'JobsPage' })

const route = useRoute()
const router = useRouter()

// The tab is in the URL so /sync/status and a shared link can open one.
const tabs = [
  { label: 'Overview', value: 'overview', slot: 'overview', icon: 'i-lucide-refresh-cw' },
  { label: 'Status', value: 'status', slot: 'status', icon: 'i-lucide-list-checks' },
  { label: 'Workers', value: 'workers', slot: 'workers', icon: 'i-lucide-server-cog' },
]

const tab = computed({
  get: () => (tabs.some((item) => item.value === route.query.tab) ? route.query.tab : 'overview'),
  set: (value) =>
    router.replace({
      query: { ...route.query, tab: value === 'overview' ? undefined : value },
    }),
})
</script>

<template>
  <UTabs v-model="tab" :items="tabs" :unmount-on-hide="false">
    <template #overview>
      <div class="pt-2">
        <SyncOverviewPage />
      </div>
    </template>
    <template #status>
      <div class="pt-2">
        <JobStatusPage section="status" />
      </div>
    </template>
    <template #workers>
      <div class="pt-2">
        <JobStatusPage section="workers" />
      </div>
    </template>
  </UTabs>
</template>
