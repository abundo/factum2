<script setup>
import { computed, onMounted, ref } from 'vue'
import { useToast } from '@nuxt/ui/composables'
import {
  createContact,
  deleteContact,
  getContact,
  getContactCustomers,
  getContacts,
  setContactCustomers,
  updateContact,
} from '@/api/contacts'
import { getCustomers } from '@/api/customers'
import SortableColumnHeader from '@/components/SortableColumnHeader.vue'
import { useAuthStore } from '@/stores/auth'

defineOptions({ name: 'ContactList' })

const toast = useToast()
const authStore = useAuthStore()

const contacts = ref([])
const customers = ref([])
const loading = ref(true)
const error = ref(null)
const globalFilter = ref('')
const sorting = ref([{ id: 'name', desc: false }])

const columns = [
  { id: 'actions', header: '' },
  { accessorKey: 'name', header: 'Name' },
  { accessorKey: 'email', header: 'Email' },
  { accessorKey: 'phone', header: 'Phone' },
  { id: 'notify', header: 'Notify' },
  { accessorKey: 'source', header: 'Source' },
]

const emptyForm = () => ({
  name: '',
  email: '',
  phone: '',
  notify_maintenance: true,
})

const detailDialog = ref(false)
const form = ref(emptyForm())
const editingId = ref(null)
const contactSource = ref('')
const selectedCustomers = ref([])
const contactLoading = ref(false)
const contactError = ref(null)
const saving = ref(false)
const deleteDialog = ref(false)
const deleting = ref(false)

const isCreate = computed(() => editingId.value === null)
const isLime = computed(() => contactSource.value === 'lime')
const isLocal = computed(() => contactSource.value !== 'lime')
const canWrite = computed(() => authStore.canWrite)
const fieldsReadOnly = computed(() => {
  if (!canWrite.value) return true
  if (isCreate.value) return false
  return isLime.value
})
const notifyReadOnly = computed(() => !canWrite.value)
const canSave = computed(() => canWrite.value && (isCreate.value || isLocal.value || isLime.value))

const dialogTitle = computed(() => {
  if (isCreate.value) return 'New contact'
  const name = form.value.name || 'Contact'
  if (isLime.value) return `${name} (synced from Lime)`
  return name
})

function loadContacts() {
  loading.value = true
  error.value = null
  Promise.all([getContacts(), getCustomers()])
    .then(([c, cust]) => {
      contacts.value = c ?? []
      customers.value = cust ?? []
    })
    .catch(() => {
      error.value = 'Failed to load contacts.'
    })
    .finally(() => {
      loading.value = false
    })
}

function openNew() {
  editingId.value = null
  contactSource.value = 'factum'
  form.value = emptyForm()
  selectedCustomers.value = []
  contactError.value = null
  contactLoading.value = false
  detailDialog.value = true
}

function showDetail(row) {
  detailDialog.value = true
  editingId.value = row.id
  contactSource.value = row.source ?? ''
  form.value = emptyForm()
  selectedCustomers.value = []
  contactError.value = null
  contactLoading.value = true
  Promise.all([getContact(row.id), getContactCustomers(row.id)])
    .then(([data, list]) => {
      editingId.value = data.id
      contactSource.value = data.source ?? ''
      form.value = {
        name: data.name ?? '',
        email: data.email ?? '',
        phone: data.phone ?? '',
        notify_maintenance: data.notify_maintenance,
      }
      selectedCustomers.value = (list ?? []).map((c) => c.id)
    })
    .catch(() => {
      contactError.value = 'Failed to load contact.'
    })
    .finally(() => {
      contactLoading.value = false
    })
}

function save() {
  if (!form.value.name.trim()) {
    toast.add({ color: 'error', title: 'Name is required' })
    return
  }
  saving.value = true
  const payload = { ...form.value }
  const req = isCreate.value ? createContact(payload) : updateContact(editingId.value, payload)
  req
    .then((row) => {
      if (isLime.value) {
        return row
      }
      return setContactCustomers(row.id, selectedCustomers.value)
    })
    .then(() => {
      detailDialog.value = false
      loadContacts()
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

function confirmDelete() {
  deleteDialog.value = true
}

function performDelete() {
  if (!editingId.value) return
  deleting.value = true
  deleteContact(editingId.value)
    .then(() => {
      deleteDialog.value = false
      detailDialog.value = false
      loadContacts()
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

onMounted(loadContacts)
</script>

<template>
  <div class="card flex min-h-0 flex-1 flex-col overflow-hidden">
    <div class="flex flex-wrap gap-2 items-center justify-between mb-4 shrink-0">
      <div class="flex items-center gap-2">
        <h4 class="m-0">Contacts</h4>
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
      :data="contacts"
      :columns="columns"
      :loading="loading"
      :empty="error ?? 'No contacts found.'"
      :virtualize="{ estimateSize: 46 }"
      sticky
      class="min-h-0 flex-1"
    >
      <template #name-header="{ column }">
        <SortableColumnHeader :column="column" label="Name" />
      </template>
      <template #email-header="{ column }">
        <SortableColumnHeader :column="column" label="Email" />
      </template>
      <template #phone-header="{ column }">
        <SortableColumnHeader :column="column" label="Phone" />
      </template>
      <template #source-header="{ column }">
        <SortableColumnHeader :column="column" label="Source" />
      </template>
      <template #notify-cell="{ row }">
        <UBadge
          :label="row.original.notify_maintenance ? 'yes' : 'no'"
          :color="row.original.notify_maintenance ? 'success' : 'neutral'"
          variant="subtle"
        />
      </template>
      <template #source-cell="{ row }">
        <UBadge
          :label="row.original.source || '—'"
          :color="sourceBadgeColor(row.original.source)"
          variant="subtle"
        />
      </template>

      <template #actions-cell="{ row }">
        <UButton
          icon="i-lucide-pencil"
          variant="outline"
          color="neutral"
          size="sm"
          @click="showDetail(row.original)"
        />
      </template>
    </UTable>
  </div>

  <UModal v-model:open="detailDialog" :title="dialogTitle" :ui="{ content: 'sm:max-w-lg' }">
    <template #body>
      <div v-if="contactLoading" class="flex justify-center p-4">
        <UIcon name="i-lucide-loader-2" class="size-8 animate-spin" />
      </div>

      <UAlert v-else-if="contactError" color="error" variant="subtle" :title="contactError" />

      <div v-else class="grid grid-cols-[9rem_1fr] items-center gap-y-4 gap-x-3">
        <label for="name" class="font-bold">Name</label>
        <UInput id="name" v-model="form.name" :disabled="fieldsReadOnly" class="w-full" />

        <label for="email" class="font-bold">Email</label>
        <UInput id="email" v-model="form.email" :disabled="fieldsReadOnly" class="w-full" />

        <label for="phone" class="font-bold">Phone</label>
        <UInput id="phone" v-model="form.phone" :disabled="fieldsReadOnly" class="w-full" />

        <span class="font-bold">Notify</span>
        <div class="flex items-center gap-2">
          <USwitch v-model="form.notify_maintenance" :disabled="notifyReadOnly" />
          <span>Notify on maintenance</span>
        </div>

        <span class="font-bold self-start mt-2">Customers</span>
        <USelectMenu
          v-model="selectedCustomers"
          multiple
          value-key="id"
          label-key="name"
          :items="customers"
          :disabled="fieldsReadOnly"
          placeholder="Link customers"
          class="w-full"
        />

        <template v-if="!isCreate">
          <label for="source" class="font-bold">Source</label>
          <UInput id="source" :model-value="contactSource" disabled class="w-full" />
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
            v-if="canSave"
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

  <UModal v-model:open="deleteDialog" title="Delete contact" :ui="{ content: 'sm:max-w-sm' }">
    <template #body>
      <p>
        Delete contact <strong>{{ form.name || 'this contact' }}</strong
        >? This cannot be undone. Linked customers are kept; only the contact and its customer links
        are removed.
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
