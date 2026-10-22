<script setup>
import { computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import DnsDNSSECPoliciesPage from './DnsDNSSECPoliciesPage.vue'
import DnsSOATemplatesPage from './DnsSOATemplatesPage.vue'
import DnsTemplatesPage from './DnsTemplatesPage.vue'
import DnsZonesPage from './DnsZonesPage.vue'

defineOptions({ name: 'DnsPage' })

const route = useRoute()
const router = useRouter()

// Same shape as portitor DnsPage: the tab is in the URL so a link can open one.
const tabs = [
  { label: 'Zones', value: 'zones', slot: 'zones', icon: 'i-lucide-globe-2' },
  { label: 'DNS templates', value: 'templates', slot: 'templates', icon: 'i-lucide-layers' },
  { label: 'SOA templates', value: 'soa', slot: 'soa', icon: 'i-lucide-file-text' },
  { label: 'DNSSEC policies', value: 'dnssec', slot: 'dnssec', icon: 'i-lucide-shield' },
]

const tab = computed({
  get: () => (tabs.some((item) => item.value === route.query.tab) ? route.query.tab : 'zones'),
  set: (value) =>
    router.replace({ query: { ...route.query, tab: value === 'zones' ? undefined : value } }),
})
</script>

<template>
  <UTabs v-model="tab" :items="tabs" :unmount-on-hide="false">
    <template #zones>
      <div class="pt-2">
        <DnsZonesPage />
      </div>
    </template>
    <template #templates>
      <div class="pt-2">
        <DnsTemplatesPage />
      </div>
    </template>
    <template #soa>
      <div class="pt-2">
        <DnsSOATemplatesPage />
      </div>
    </template>
    <template #dnssec>
      <div class="pt-2">
        <DnsDNSSECPoliciesPage />
      </div>
    </template>
  </UTabs>
</template>
