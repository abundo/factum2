<script setup>
import { onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { getCustomer, getCustomers } from '@/api/customers'
import SortableColumnHeader from '@/components/SortableColumnHeader.vue'

defineOptions({ name: 'CustomerList' })

const router = useRouter()

const customers = ref([])
const loading = ref(true)
const error = ref(null)
const globalFilter = ref('')
const sorting = ref([{ id: 'name', desc: false }])

const columns = [
  { id: 'actions', header: '' },
  { accessorKey: 'name', header: 'Name' },
  { accessorKey: 'postalcity', header: 'City' },
  { accessorKey: 'organization_number', header: 'Org.no' },
  { accessorKey: 'country', header: 'Country' },
]

const detailDialog = ref(false)
const customer = ref(null)
const customerLoading = ref(false)
const customerError = ref(null)

function loadCustomers() {
  loading.value = true
  error.value = null
  getCustomers()
    .then((data) => {
      customers.value = data ?? []
    })
    .catch(() => {
      error.value = 'Failed to load customers.'
    })
    .finally(() => {
      loading.value = false
    })
}

function showDetail(row) {
  detailDialog.value = true
  customer.value = null
  customerError.value = null
  customerLoading.value = true
  getCustomer(row.id)
    .then((data) => {
      customer.value = data
    })
    .catch(() => {
      customerError.value = 'Failed to load customer.'
    })
    .finally(() => {
      customerLoading.value = false
    })
}

function showServices(customerRow) {
  router.push({ path: '/service', query: { customer_id: customerRow.id } })
}

onMounted(loadCustomers)
</script>

<template>
  <div class="card">
    <div class="flex flex-wrap gap-2 items-center justify-between mb-4">
      <h4 class="m-0">Customers</h4>
      <UInput v-model="globalFilter" icon="i-lucide-search" placeholder="Search..." />
    </div>

    <UTable
      v-model:sorting="sorting"
      v-model:global-filter="globalFilter"
      :data="customers"
      :columns="columns"
      :loading="loading"
      :empty="error ?? 'No customers found.'"
      :virtualize="{ estimateSize: 46 }"
      class="max-h-[calc(100vh-320px)]"
    >
      <template #name-header="{ column }">
        <SortableColumnHeader :column="column" label="Name" />
      </template>
      <template #postalcity-header="{ column }">
        <SortableColumnHeader :column="column" label="City" />
      </template>
      <template #organization_number-header="{ column }">
        <SortableColumnHeader :column="column" label="Org.no" />
      </template>
      <template #country-header="{ column }">
        <SortableColumnHeader :column="column" label="Country" />
      </template>

      <template #actions-cell="{ row }">
        <div class="flex gap-2">
          <UButton
            icon="i-lucide-pencil"
            variant="outline"
            color="neutral"
            size="sm"
            @click="showDetail(row.original)"
          />
          <UButton
            label="Services"
            size="sm"
            color="neutral"
            variant="outline"
            @click="showServices(row.original)"
          />
        </div>
      </template>
    </UTable>
  </div>

  <UModal
    v-model:open="detailDialog"
    :title="customer?.name ? `${customer.name} (synced from Lime, read-only)` : 'Customer'"
    :ui="{ content: 'sm:max-w-lg' }"
  >
    <template #body>
      <div v-if="customerLoading" class="flex justify-center p-4">
        <UIcon name="i-lucide-loader-2" class="size-8 animate-spin" />
      </div>

      <UAlert v-else-if="customerError" color="error" variant="subtle" :title="customerError" />

      <div v-else-if="customer" class="grid grid-cols-[9rem_1fr] items-center gap-y-4 gap-x-3">
        <label for="name" class="font-bold">Name</label>
        <UInput id="name" :model-value="customer.name" disabled class="w-full" />

        <label for="postal_address1" class="font-bold">Postal address 1</label>
        <UInput id="postal_address1" :model-value="customer.postal_address1" disabled class="w-full" />

        <label for="postal_address2" class="font-bold">Postal address 2</label>
        <UInput id="postal_address2" :model-value="customer.postal_address2" disabled class="w-full" />

        <label for="postalzipcode" class="font-bold">Zip code</label>
        <UInput id="postalzipcode" :model-value="customer.postalzipcode" disabled class="w-full" />

        <label for="postalcity" class="font-bold">City</label>
        <UInput id="postalcity" :model-value="customer.postalcity" disabled class="w-full" />

        <label for="country" class="font-bold">Country</label>
        <UInput id="country" :model-value="customer.country" disabled class="w-full" />

        <label for="organization_number" class="font-bold">Org.no</label>
        <UInput
          id="organization_number"
          :model-value="customer.organization_number"
          disabled
          class="w-full"
        />

        <template v-if="customer.source">
          <label for="source" class="font-bold">Source</label>
          <UInput id="source" :model-value="customer.source" disabled class="w-full" />
        </template>
      </div>
    </template>

    <template #footer>
      <UButton label="Close" icon="i-lucide-x" variant="ghost" @click="detailDialog = false" />
    </template>
  </UModal>
</template>
