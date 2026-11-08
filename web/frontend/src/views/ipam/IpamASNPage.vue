<script setup>
import { computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { listASNRanges, listASNs } from '@/api/ipam'
import { valuesText } from '@/utils/search'
import IpamMirrorTable from './IpamMirrorTable.vue'

defineOptions({ name: 'IpamASNPage' })

const route = useRoute()
const router = useRouter()

const tabs = [
  { label: 'ASN ranges', value: 'ranges', slot: 'ranges', icon: 'i-lucide-list' },
  { label: 'ASNs', value: 'asns', slot: 'asns', icon: 'i-lucide-hash' },
]

const tab = computed({
  get: () => (tabs.some((item) => item.value === route.query.tab) ? route.query.tab : 'ranges'),
  set: (value) =>
    router.replace({ query: { ...route.query, tab: value === 'ranges' ? undefined : value } }),
})

const rangeColumns = [
  { accessorKey: 'name', header: 'Name' },
  { accessorKey: 'slug', header: 'Slug' },
  { accessorKey: 'rir', header: 'RIR' },
  { accessorKey: 'start', header: 'Start' },
  { accessorKey: 'end', header: 'End' },
  { accessorKey: 'tenant', header: 'Tenant' },
  { accessorKey: 'description', header: 'Description' },
  { accessorKey: 'source', header: 'Source' },
]

const asnColumns = [
  { accessorKey: 'asn', header: 'ASN' },
  { accessorKey: 'rir', header: 'RIR' },
  { accessorKey: 'role', header: 'Role' },
  { accessorKey: 'tenant', header: 'Tenant' },
  { accessorKey: 'sites', header: 'Sites' },
  { accessorKey: 'description', header: 'Description' },
  { accessorKey: 'source', header: 'Source' },
]

function rangeText(row) {
  return valuesText(row.name, row.slug, row.rir, row.start, row.end, row.tenant, row.description, row.source)
}

function asnText(row) {
  return valuesText(row.asn, row.rir, row.role, row.tenant, row.sites, row.description, row.source)
}
</script>

<template>
  <UTabs v-model="tab" :items="tabs" :unmount-on-hide="false" class="flex min-h-0 flex-1 flex-col">
    <template #ranges>
      <div class="flex min-h-0 flex-1 flex-col pt-2">
        <IpamMirrorTable
          title="ASN ranges"
          :load="listASNRanges"
          :columns="rangeColumns"
          :search-text="rangeText"
          empty="No ASN ranges yet. They appear after a NetBox sync."
          failed="Failed to load ASN ranges."
        />
      </div>
    </template>
    <template #asns>
      <div class="flex min-h-0 flex-1 flex-col pt-2">
        <IpamMirrorTable
          title="ASNs"
          :load="listASNs"
          :columns="asnColumns"
          :search-text="asnText"
          empty="No ASNs yet. They appear after a NetBox sync."
          failed="Failed to load ASNs."
        />
      </div>
    </template>
  </UTabs>
</template>
