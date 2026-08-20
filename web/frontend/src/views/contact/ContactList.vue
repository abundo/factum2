<script setup>
import { onMounted, ref } from 'vue'
import { useToast } from '@nuxt/ui/composables'
import {
  createContact,
  getContactCustomers,
  getContacts,
  setContactCustomers,
  updateContact,
} from '@/api/contacts'
import { getCustomers } from '@/api/customers'
import { useAuthStore } from '@/stores/auth'

const toast = useToast()
const authStore = useAuthStore()
const contacts = ref([])
const customers = ref([])
const loading = ref(true)
const dialog = ref(false)
const form = ref({ name: '', email: '', phone: '', notify_maintenance: true })
const editingId = ref(null)
const selectedCustomers = ref([])

const columns = [
  { accessorKey: 'name', header: 'Name' },
  { accessorKey: 'email', header: 'Email' },
  { accessorKey: 'phone', header: 'Phone' },
  { id: 'notify', header: 'Notify' },
  { id: 'actions', header: '' },
]

function load() {
  loading.value = true
  Promise.all([getContacts(), getCustomers()])
    .then(([c, cust]) => {
      contacts.value = c ?? []
      customers.value = cust ?? []
    })
    .finally(() => {
      loading.value = false
    })
}

function openNew() {
  editingId.value = null
  form.value = { name: '', email: '', phone: '', notify_maintenance: true }
  selectedCustomers.value = []
  dialog.value = true
}

function openEdit(row) {
  editingId.value = row.id
  form.value = {
    name: row.name,
    email: row.email,
    phone: row.phone,
    notify_maintenance: row.notify_maintenance,
  }
  getContactCustomers(row.id).then((list) => {
    selectedCustomers.value = (list ?? []).map((c) => c.id)
    dialog.value = true
  })
}

function save() {
  const payload = { ...form.value }
  const req = editingId.value
    ? updateContact(editingId.value, payload)
    : createContact(payload)
  req
    .then((row) => setContactCustomers(row.id, selectedCustomers.value))
    .then(() => {
      dialog.value = false
      load()
    })
    .catch((err) => {
      toast.add({ color: 'error', title: 'Save failed', description: err?.response?.data?.error })
    })
}

onMounted(load)
</script>

<template>
  <div class="card">
    <div class="flex justify-between items-center mb-4">
      <h4 class="m-0">Contacts</h4>
      <UButton v-if="authStore.canWrite" label="Add contact" icon="i-lucide-plus" @click="openNew" />
    </div>
    <UTable :data="contacts" :columns="columns" :loading="loading">
      <template #notify-cell="{ row }">
        <UBadge
          :label="row.original.notify_maintenance ? 'yes' : 'no'"
          :color="row.original.notify_maintenance ? 'success' : 'neutral'"
          variant="subtle"
        />
      </template>
      <template #actions-cell="{ row }">
        <UButton
          v-if="authStore.canWrite"
          icon="i-lucide-pencil"
          size="sm"
          variant="outline"
          color="neutral"
          @click="openEdit(row.original)"
        />
      </template>
    </UTable>
  </div>

  <UModal v-model:open="dialog" :title="editingId ? 'Edit contact' : 'New contact'">
    <template #body>
      <div class="flex flex-col gap-4">
        <UInput v-model="form.name" placeholder="Name" />
        <UInput v-model="form.email" placeholder="Email" />
        <UInput v-model="form.phone" placeholder="Phone" />
        <div class="flex items-center gap-2">
          <USwitch v-model="form.notify_maintenance" />
          <span>Notify on maintenance</span>
        </div>
        <div>
          <div class="font-bold mb-2">Customers</div>
          <USelectMenu
            v-model="selectedCustomers"
            multiple
            value-key="id"
            label-key="name"
            :items="customers"
            placeholder="Link customers"
          />
        </div>
      </div>
    </template>
    <template #footer>
      <UButton label="Save" @click="save" />
    </template>
  </UModal>
</template>
