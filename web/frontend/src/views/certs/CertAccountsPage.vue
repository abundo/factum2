<script setup>
import { onMounted, reactive, ref } from 'vue'
import { useToast } from '@nuxt/ui/composables'
import { useAuthStore } from '@/stores/auth'
import { useConfirm } from '@/composables/useConfirm'
import SearchInput from '@/components/SearchInput.vue'
import { useSearch, valuesText } from '@/utils/search'
import {
  createCertAccount,
  deleteCertAccount,
  listCertAccounts,
  updateCertAccount,
} from '@/api/certs'

defineOptions({ name: 'CertAccountsPage' })

const toast = useToast()
const authStore = useAuthStore()
const { confirmDelete } = useConfirm()
const items = ref([])
const dialog = ref(false)
const saving = ref(false)
const deleting = ref(false)
const editing = ref(null)
const form = reactive(emptyForm())

const columns = [
  { id: 'actions', header: '' },
  { accessorKey: 'name', header: 'Name' },
  { accessorKey: 'email', header: 'Email' },
  { accessorKey: 'server', header: 'ACME server' },
  { accessorKey: 'key_type', header: 'Key type' },
]
const { search, filtered } = useSearch(items, (row) =>
  valuesText(row.name, row.email, row.server, row.key_type),
)

const keyTypeItems = [
  { label: 'EC256', value: 'EC256' },
  { label: 'EC384', value: 'EC384' },
  { label: 'RSA2048', value: 'RSA2048' },
  { label: 'RSA4096', value: 'RSA4096' },
]

function emptyForm() {
  return {
    name: '',
    email: '',
    server: 'https://acme-v02.api.letsencrypt.org/directory',
    key_type: 'EC256',
    accepts_terms_of_service: true,
    eab_kid: '',
    eab_hmac_key: '',
  }
}

function errMsg(err, fallback) {
  return err.response?.data?.error ?? fallback
}

async function load() {
  try {
    items.value = (await listCertAccounts()) ?? []
  } catch (err) {
    toast.add({ title: errMsg(err, 'Failed to load accounts'), color: 'error' })
  }
}

function openCreate() {
  editing.value = null
  Object.assign(form, emptyForm())
  dialog.value = true
}

function openEdit(row) {
  editing.value = row
  Object.assign(form, {
    name: row.name,
    email: row.email,
    server: row.server,
    key_type: row.key_type || 'EC256',
    accepts_terms_of_service: !!row.accepts_terms_of_service,
    eab_kid: row.eab_kid || '',
    eab_hmac_key: row.eab_hmac_key || '',
  })
  dialog.value = true
}

async function save() {
  saving.value = true
  try {
    const payload = { ...form }
    if (editing.value) {
      await updateCertAccount(editing.value.id, payload)
    } else {
      await createCertAccount(payload)
    }
    dialog.value = false
    await load()
    toast.add({ title: 'Saved', color: 'success' })
  } catch (err) {
    toast.add({ title: errMsg(err, 'Failed to save account'), color: 'error' })
  } finally {
    saving.value = false
  }
}

async function remove() {
  if (!editing.value) return
  if (!(await confirmDelete(`account ${editing.value.name}`))) return
  deleting.value = true
  try {
    await deleteCertAccount(editing.value.id)
    dialog.value = false
    await load()
  } catch (err) {
    toast.add({ title: errMsg(err, 'Failed to delete account'), color: 'error' })
  } finally {
    deleting.value = false
  }
}

onMounted(load)
</script>

<template>
  <div class="card">
    <div class="flex items-center justify-between mb-4">
      <div>
        <div class="font-semibold text-lg">ACME accounts</div>
        <p class="text-muted-color text-sm">Let’s Encrypt (or other ACME) accounts used by lego.</p>
      </div>
      <UButton
        v-if="authStore.canWrite"
        icon="i-lucide-plus"
        label="New account"
        @click="openCreate"
      />
    </div>
    <SearchInput v-model="search" class="mb-3 max-w-xs" />
    <UTable
      :data="filtered"
      :columns="columns"
      :empty="search && items.length ? 'Nothing matches the search.' : 'No accounts found.'"
    >
      <template #actions-cell="{ row }">
        <UButton
          v-if="authStore.canWrite"
          size="sm"
          variant="outline"
          icon="i-lucide-pencil"
          @click="openEdit(row.original)"
        />
      </template>
    </UTable>
  </div>

  <FormModal v-model:open="dialog" :source="form" :title="editing ? 'Edit account' : 'New account'">
    <template #body>
      <form id="cert-account-form" class="space-y-3" @submit.prevent="save">
        <UFormField label="Name">
          <UInput v-model="form.name" class="w-full" required />
        </UFormField>
        <UFormField label="Email">
          <UInput v-model="form.email" type="email" class="w-full" />
        </UFormField>
        <UFormField label="ACME server">
          <UInput v-model="form.server" class="w-full font-mono" />
        </UFormField>
        <UFormField label="Account key type">
          <USelect v-model="form.key_type" :items="keyTypeItems" class="w-full" />
        </UFormField>
        <div class="flex items-center gap-2">
          <USwitch v-model="form.accepts_terms_of_service" id="tos" />
          <label for="tos">Accept terms of service</label>
        </div>
        <UFormField label="EAB KID (optional)">
          <UInput v-model="form.eab_kid" class="w-full" />
        </UFormField>
        <UFormField label="EAB HMAC key (optional)">
          <UInput v-model="form.eab_hmac_key" class="w-full" />
        </UFormField>
      </form>
    </template>
    <template #footer="{ close }">
      <div class="flex w-full gap-2">
        <UButton
          v-if="editing && authStore.canWrite"
          color="error"
          variant="ghost"
          icon="i-lucide-trash"
          label="Delete"
          type="button"
          :loading="deleting"
          @click="remove"
        />
        <UButton class="ms-auto" color="neutral" variant="ghost" type="button" @click="close"
          >Cancel</UButton
        >
        <UButton type="submit" form="cert-account-form" :loading="saving">Save</UButton>
      </div>
    </template>
  </FormModal>
</template>
