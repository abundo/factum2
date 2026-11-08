<script setup>
import { listRouteTargets } from '@/api/ipam'
import { valuesText } from '@/utils/search'
import IpamMirrorTable from './IpamMirrorTable.vue'

defineOptions({ name: 'IpamRouteTargetList' })

const columns = [
  { accessorKey: 'name', header: 'Name' },
  { accessorKey: 'tenant', header: 'Tenant' },
  { accessorKey: 'description', header: 'Description' },
  { accessorKey: 'source', header: 'Source' },
]

function rowText(row) {
  return valuesText(row.name, row.tenant, row.description, row.source)
}
</script>

<template>
  <IpamMirrorTable
    title="Route targets"
    :load="listRouteTargets"
    :columns="columns"
    :search-text="rowText"
    empty="No route targets yet. They appear after a NetBox sync."
    failed="Failed to load route targets."
  />
</template>
