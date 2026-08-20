<script setup>
import { computed, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { getCustomers } from '@/api/customers'
import { getServices } from '@/api/services'
import ServiceEditDialog from '@/components/ServiceEditDialog.vue'
import SortableColumnHeader from '@/components/SortableColumnHeader.vue'
import { useAuthStore } from '@/stores/auth'

const route = useRoute()
const router = useRouter()
const authStore = useAuthStore()

const services = ref([])
const customers = ref([])
const loading = ref(true)
const error = ref(null)

const serviceDialogOpen = ref(false)
const editingServiceId = ref(null)

const globalFilter = ref('')
const sorting = ref([{ id: 'service_id', desc: false }])

const columns = [
  { id: 'actions', header: '' },
  { accessorKey: 'service_id', header: 'Service ID' },
  { accessorKey: 'service_type', header: 'Category' },
  { accessorKey: 'bandwidth_mbps', header: 'Bandwidth (Mbps)' },
  { accessorKey: 'company', header: 'Company' },
  { accessorKey: 'deliverypoint1', header: 'Deliverypoint A' },
  { accessorKey: 'deliverypoint2', header: 'Deliverypoint B' },
  { accessorKey: 'product', header: 'Product' },
  { accessorKey: 'service', header: 'Service' },
]

const customerId = computed(() => {
  const id = Number(route.query.customer_id)
  return Number.isInteger(id) && id > 0 ? id : null
})

const filteredCustomerName = computed(() => {
  if (!customerId.value) return null
  return customers.value.find((c) => c.id === customerId.value)?.name ?? `#${customerId.value}`
})

function loadServices() {
  loading.value = true
  error.value = null
  getServices(customerId.value)
    .then((data) => {
      services.value = data ?? []
    })
    .catch(() => {
      error.value = 'Failed to load services.'
    })
    .finally(() => {
      loading.value = false
    })
}

function loadCustomers() {
  getCustomers()
    .then((data) => {
      customers.value = data ?? []
    })
    .catch(() => {
      // Customer names are only needed for the filter badge, not critical for the list.
    })
}

function clearCustomerFilter() {
  router.push({ path: '/service' })
}

function openNew() {
  router.push({ path: '/service/new' })
}

function editService(row) {
  editingServiceId.value = row.id
  serviceDialogOpen.value = true
}

watch(customerId, loadServices)

onMounted(() => {
  loadServices()
  loadCustomers()
})
</script>

<template>
  <div class="card">
    <div v-if="customerId" class="flex justify-end mb-6">
      <div class="flex items-center gap-2">
        <UBadge :label="`Customer: ${filteredCustomerName}`" variant="subtle" />
        <UButton
          label="Clear filter"
          icon="i-lucide-x"
          variant="ghost"
          size="sm"
          @click="clearCustomerFilter"
        />
      </div>
    </div>

    <div class="flex flex-wrap gap-2 items-center justify-between mb-4">
      <div class="flex items-center gap-2">
        <h4 class="m-0">Services</h4>
        <UButton
          v-if="authStore.canWrite"
          label="New"
          icon="i-lucide-plus"
          color="neutral"
          size="sm"
          @click="openNew"
        />
      </div>
      <UInput v-model="globalFilter" icon="i-lucide-search" placeholder="Search..." />
    </div>

    <UTable
      v-model:sorting="sorting"
      v-model:global-filter="globalFilter"
      :data="services"
      :columns="columns"
      :loading="loading"
      :empty="error ?? 'No services found.'"
      :virtualize="{ estimateSize: 46 }"
      sticky
      class="max-h-[calc(100vh-380px)]"
    >
      <template
        v-for="col in columns.filter((c) => c.id !== 'actions')"
        :key="col.accessorKey"
        #[`${col.accessorKey}-header`]="{ column }"
      >
        <SortableColumnHeader :column="column" :label="col.header" />
      </template>

      <template #actions-cell="{ row }">
        <UButton
          icon="i-lucide-pencil"
          variant="outline"
          color="neutral"
          size="sm"
          @click="editService(row.original)"
        />
      </template>
    </UTable>
  </div>

  <ServiceEditDialog
    v-model:open="serviceDialogOpen"
    :service-id="editingServiceId"
    @saved="loadServices"
    @deleted="loadServices"
  />
</template>
