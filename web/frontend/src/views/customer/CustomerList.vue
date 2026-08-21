<script setup>
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { useToast } from '@nuxt/ui/composables'
import {
  createCustomer,
  deleteCustomer,
  getCustomer,
  getCustomers,
  updateCustomer,
} from '@/api/customers'
import SortableColumnHeader from '@/components/SortableColumnHeader.vue'
import { useAuthStore } from '@/stores/auth'

defineOptions({ name: 'CustomerList' })

const router = useRouter()
const toast = useToast()
const authStore = useAuthStore()

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
  { accessorKey: 'source', header: 'Source' },
]

const emptyForm = () => ({
  name: '',
  postal_address1: '',
  postal_address2: '',
  postalzipcode: '',
  postalcity: '',
  country: '',
  organization_number: '',
})

const detailDialog = ref(false)
const form = ref(emptyForm())
const editingId = ref(null)
const customerSource = ref('')
const customerLoading = ref(false)
const customerError = ref(null)
const saving = ref(false)
const deleteDialog = ref(false)
const deleting = ref(false)

const isCreate = computed(() => editingId.value === null)
const isLime = computed(() => customerSource.value === 'lime')
const isLocal = computed(() => customerSource.value === 'factum')
const canWrite = computed(() => authStore.canWrite)
const readOnly = computed(() => {
  if (!canWrite.value) return true
  if (isCreate.value) return false
  return !isLocal.value
})

const dialogTitle = computed(() => {
  if (isCreate.value) return 'New customer'
  const name = form.value.name || 'Customer'
  if (isLime.value) return `${name} (synced from Lime, read-only)`
  return name
})

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

function openNew() {
  editingId.value = null
  customerSource.value = 'factum'
  form.value = emptyForm()
  customerError.value = null
  customerLoading.value = false
  detailDialog.value = true
}

function showDetail(row) {
  detailDialog.value = true
  editingId.value = row.id
  customerSource.value = row.source ?? ''
  form.value = emptyForm()
  customerError.value = null
  customerLoading.value = true
  getCustomer(row.id)
    .then((data) => {
      editingId.value = data.id
      customerSource.value = data.source ?? ''
      form.value = {
        name: data.name ?? '',
        postal_address1: data.postal_address1 ?? '',
        postal_address2: data.postal_address2 ?? '',
        postalzipcode: data.postalzipcode ?? '',
        postalcity: data.postalcity ?? '',
        country: data.country ?? '',
        organization_number: data.organization_number ?? '',
      }
    })
    .catch(() => {
      customerError.value = 'Failed to load customer.'
    })
    .finally(() => {
      customerLoading.value = false
    })
}

function save() {
  if (!form.value.name.trim()) {
    toast.add({ color: 'error', title: 'Name is required' })
    return
  }
  saving.value = true
  const payload = { ...form.value }
  const req = isCreate.value ? createCustomer(payload) : updateCustomer(editingId.value, payload)
  req
    .then(() => {
      detailDialog.value = false
      loadCustomers()
    })
    .catch((err) => {
      toast.add({
        color: 'error',
        title: isCreate.value ? 'Create failed' : 'Save failed',
        description: err?.response?.data?.error,
      })
    })
    .finally(() => {
      saving.value = false
    })
}

function showServices(customerRow) {
  router.push({ path: '/service', query: { customer_id: customerRow.id } })
}

function confirmDelete() {
  deleteDialog.value = true
}

function performDelete() {
  if (!editingId.value) return
  deleting.value = true
  deleteCustomer(editingId.value)
    .then(() => {
      deleteDialog.value = false
      detailDialog.value = false
      loadCustomers()
    })
    .catch((err) => {
      deleteDialog.value = false
      toast.add({
        color: 'error',
        title: 'Delete failed',
        description: err?.response?.data?.error,
      })
    })
    .finally(() => {
      deleting.value = false
    })
}

function sourceBadgeColor(source) {
  if (source === 'factum') return 'success'
  if (source === 'lime') return 'neutral'
  return 'neutral'
}

onMounted(loadCustomers)
</script>

<template>
  <div class="card flex min-h-0 flex-1 flex-col overflow-hidden">
    <div class="flex flex-wrap gap-2 items-center justify-between mb-4 shrink-0">
      <div class="flex items-center gap-2">
        <h4 class="m-0">Customers</h4>
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
      :data="customers"
      :columns="columns"
      :loading="loading"
      :empty="error ?? 'No customers found.'"
      :virtualize="{ estimateSize: 46 }"
      sticky
      class="min-h-0 flex-1"
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
      <template #source-header="{ column }">
        <SortableColumnHeader :column="column" label="Source" />
      </template>
      <template #source-cell="{ row }">
        <UBadge
          :label="row.original.source || '—'"
          :color="sourceBadgeColor(row.original.source)"
          variant="subtle"
        />
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
            v-if="row.original.service_count > 0"
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

  <UModal v-model:open="detailDialog" :title="dialogTitle" :ui="{ content: 'sm:max-w-lg' }">
    <template #body>
      <div v-if="customerLoading" class="flex justify-center p-4">
        <UIcon name="i-lucide-loader-2" class="size-8 animate-spin" />
      </div>

      <UAlert v-else-if="customerError" color="error" variant="subtle" :title="customerError" />

      <div v-else class="grid grid-cols-[9rem_1fr] items-center gap-y-4 gap-x-3">
        <label for="name" class="font-bold">Name</label>
        <UInput id="name" v-model="form.name" :disabled="readOnly" class="w-full" />

        <label for="postal_address1" class="font-bold">Postal address 1</label>
        <UInput
          id="postal_address1"
          v-model="form.postal_address1"
          :disabled="readOnly"
          class="w-full"
        />

        <label for="postal_address2" class="font-bold">Postal address 2</label>
        <UInput
          id="postal_address2"
          v-model="form.postal_address2"
          :disabled="readOnly"
          class="w-full"
        />

        <label for="postalzipcode" class="font-bold">Zip code</label>
        <UInput
          id="postalzipcode"
          v-model="form.postalzipcode"
          :disabled="readOnly"
          class="w-full"
        />

        <label for="postalcity" class="font-bold">City</label>
        <UInput id="postalcity" v-model="form.postalcity" :disabled="readOnly" class="w-full" />

        <label for="country" class="font-bold">Country</label>
        <UInput id="country" v-model="form.country" :disabled="readOnly" class="w-full" />

        <label for="organization_number" class="font-bold">Org.no</label>
        <UInput
          id="organization_number"
          v-model="form.organization_number"
          :disabled="readOnly"
          class="w-full"
        />

        <template v-if="!isCreate">
          <label for="source" class="font-bold">Source</label>
          <UInput id="source" :model-value="customerSource" disabled class="w-full" />
        </template>
      </div>
    </template>

    <template #footer>
      <div class="flex w-full justify-between">
        <UButton
          v-if="!isCreate && isLocal && canWrite"
          label="Delete"
          icon="i-lucide-trash-2"
          variant="outline"
          color="error"
          @click="confirmDelete"
        />
        <div class="flex gap-2 ms-auto">
          <UButton
            v-if="!readOnly"
            :label="isCreate ? 'Create' : 'Save'"
            :loading="saving"
            :disabled="!form.name.trim() || saving"
            @click="save"
          />
          <UButton label="Close" icon="i-lucide-x" variant="ghost" @click="detailDialog = false" />
        </div>
      </div>
    </template>
  </UModal>

  <UModal v-model:open="deleteDialog" title="Delete customer" :ui="{ content: 'sm:max-w-sm' }">
    <template #body>
      <p>
        Delete customer <strong>{{ form.name || 'this customer' }}</strong
        >? This cannot be undone. Customers that still have services cannot be deleted.
      </p>
    </template>
    <template #footer>
      <UButton label="Cancel" icon="i-lucide-x" variant="ghost" @click="deleteDialog = false" />
      <UButton
        label="Delete"
        icon="i-lucide-trash-2"
        color="error"
        :loading="deleting"
        @click="performDelete"
      />
    </template>
  </UModal>
</template>
