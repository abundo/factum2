<script setup>
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { useToast } from '@nuxt/ui/composables'
import {
  createCustomer,
  deleteCustomer,
  getCustomer,
  getCustomerContacts,
  getCustomers,
  updateCustomer,
} from '@/api/customers'
import SearchInput from '@/components/SearchInput.vue'
import SortableColumnHeader from '@/components/SortableColumnHeader.vue'
import { useConfirm } from '@/composables/useConfirm'
import { useAuthStore } from '@/stores/auth'
import { useSearch, valuesText } from '@/utils/search'

defineOptions({ name: 'CustomerList' })

const router = useRouter()
const toast = useToast()
const authStore = useAuthStore()
const { confirmDelete } = useConfirm()

const customers = ref([])
const loading = ref(true)
const error = ref(null)
const sorting = ref([{ id: 'name', desc: false }])
const { search, filtered } = useSearch(customers, (row) =>
  valuesText(
    row.name,
    row.postalcity,
    row.organization_number,
    row.country,
    row.source,
    row.service_count > 0 ? 'Services' : '',
  ),
)

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
const relatedContacts = ref([])
const saving = ref(false)
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
  relatedContacts.value = []
  customerError.value = null
  customerLoading.value = false
  detailDialog.value = true
}

function showDetail(row) {
  detailDialog.value = true
  editingId.value = row.id
  customerSource.value = row.source ?? ''
  form.value = emptyForm()
  relatedContacts.value = []
  customerError.value = null
  customerLoading.value = true
  Promise.all([getCustomer(row.id), getCustomerContacts(row.id)])
    .then(([data, contacts]) => {
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
      relatedContacts.value = [...(contacts ?? [])].sort((a, b) =>
        (a.name ?? '').localeCompare(b.name ?? ''),
      )
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

async function performDelete() {
  if (!editingId.value) return
  const ok = await confirmDelete(
    `customer ${form.value.name || 'this customer'}`,
    'This cannot be undone. Customers that still have services cannot be deleted.',
  )
  if (!ok) return
  deleting.value = true
  deleteCustomer(editingId.value)
    .then(() => {
      detailDialog.value = false
      loadCustomers()
    })
    .catch((err) => {
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

function telHref(phone) {
  return `tel:${phone.trim().replace(/[^\d+]/g, '')}`
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
      <SearchInput v-model="search" />
    </div>

    <UTable
      v-model:sorting="sorting"
      :data="filtered"
      :columns="columns"
      :loading="loading"
      :empty="
        error || (search && customers.length ? 'Nothing matches the search.' : 'No customers found.')
      "
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

  <FormModal
    v-model:open="detailDialog"
    :source="form"
    :loading="customerLoading"
    :title="dialogTitle"
  >
    <template #body>
      <div v-if="customerLoading" class="flex justify-center p-4">
        <UIcon name="i-lucide-loader-2" class="size-8 animate-spin" />
      </div>

      <UAlert v-else-if="customerError" color="error" variant="subtle" :title="customerError" />

      <div v-else>
        <div class="flex flex-col gap-4">
          <UFormField label="Name">
            <UInput v-model="form.name" :disabled="readOnly" class="w-full" />
          </UFormField>
          <UFormField label="Postal address 1">
            <UInput v-model="form.postal_address1" :disabled="readOnly" class="w-full" />
          </UFormField>
          <UFormField label="Postal address 2">
            <UInput v-model="form.postal_address2" :disabled="readOnly" class="w-full" />
          </UFormField>
          <UFormField label="Zip code">
            <UInput v-model="form.postalzipcode" :disabled="readOnly" class="w-full" />
          </UFormField>
          <UFormField label="City">
            <UInput v-model="form.postalcity" :disabled="readOnly" class="w-full" />
          </UFormField>
          <UFormField label="Country">
            <UInput v-model="form.country" :disabled="readOnly" class="w-full" />
          </UFormField>
          <UFormField label="Org.no">
            <UInput v-model="form.organization_number" :disabled="readOnly" class="w-full" />
          </UFormField>
          <UFormField v-if="!isCreate" label="Source">
            <UInput :model-value="customerSource" disabled class="w-full" />
          </UFormField>
        </div>

        <div v-if="!isCreate" class="mt-6 border-t border-default pt-4">
          <p class="font-bold mb-2">Contacts</p>
          <p v-if="relatedContacts.length === 0" class="text-sm text-muted-color">
            No contacts linked to this customer.
          </p>
          <ul v-else class="flex flex-col gap-3">
            <li v-for="contact in relatedContacts" :key="contact.id">
              <div class="font-medium">{{ contact.name || '—' }}</div>
              <div class="text-sm text-muted-color">
                <a
                  v-if="contact.email?.trim()"
                  :href="`mailto:${contact.email.trim()}`"
                  class="text-primary hover:underline"
                  >{{ contact.email.trim() }}</a
                >
                <span v-else>—</span>
                <span v-if="contact.phone?.trim()">
                  ·
                  <a :href="telHref(contact.phone)" class="text-primary hover:underline">{{
                    contact.phone.trim()
                  }}</a>
                </span>
              </div>
            </li>
          </ul>
        </div>
      </div>
    </template>

    <template #footer="{ close }">
      <div class="flex w-full gap-2">
        <UButton
          v-if="!isCreate && isLocal && canWrite"
          label="Delete"
          icon="i-lucide-trash"
          color="error"
          variant="ghost"
          :loading="deleting"
          @click="performDelete"
        />
        <UButton class="ms-auto" label="Cancel" icon="i-lucide-x" variant="ghost" @click="close" />
        <UButton
          v-if="!readOnly"
          :label="isCreate ? 'Create' : 'Save'"
          :loading="saving"
          :disabled="!form.name.trim() || saving"
          @click="save"
        />
      </div>
    </template>
  </FormModal>
</template>
