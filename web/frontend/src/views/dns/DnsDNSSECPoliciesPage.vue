<script setup>
import { onMounted, reactive, ref } from 'vue'
import { useToast } from '@nuxt/ui/composables'
import { useAuthStore } from '@/stores/auth'
import { useConfirm } from '@/composables/useConfirm'
import SearchInput from '@/components/SearchInput.vue'
import {
  createDNSSECPolicy,
  deleteDNSSECPolicy,
  listDNSSECPolicies,
  updateDNSSECPolicy,
} from '@/api/dns'
import { useSearch, valuesText } from '@/utils/search'

defineOptions({ name: 'DnsDNSSECPoliciesPage' })

const toast = useToast()
const authStore = useAuthStore()
const { confirmDelete } = useConfirm()
const items = ref([])
const dialog = ref(false)
const saving = ref(false)
const deleting = ref(false)
const editing = ref(null)
const form = reactive(emptyForm())

const algorithms = [
  'ecdsap256sha256',
  'ecdsap384sha384',
  'ed25519',
  'ed448',
  'rsasha256',
  'rsasha512',
]
const algorithmItems = algorithms.map((a) => ({ label: a, value: a }))

const columns = [
  { id: 'actions', header: '' },
  { accessorKey: 'name', header: 'Name' },
  { accessorKey: 'ksk_algorithm', header: 'KSK' },
  { accessorKey: 'zsk_algorithm', header: 'ZSK' },
  { accessorKey: 'signatures_validity', header: 'Signatures' },
]
const { search, filtered } = useSearch(items, (row) =>
  valuesText(row.name, row.ksk_algorithm, row.zsk_algorithm, row.signatures_validity),
)

function emptyForm() {
  return {
    name: '',
    ksk_lifetime: 'P1y',
    ksk_algorithm: 'ecdsap384sha384',
    zsk_lifetime: '30d',
    zsk_algorithm: 'ecdsap384sha384',
    purge_keys: '365',
    signatures_validity: '30d',
    signatures_validity_dnskey: '30d',
    signatures_refresh: '20d',
  }
}

function errMsg(err, fallback) {
  return err.response?.data?.error ?? fallback
}

async function load() {
  try {
    items.value = (await listDNSSECPolicies()) ?? []
  } catch (err) {
    toast.add({ title: errMsg(err, 'Failed to load DNSSEC policies'), color: 'error' })
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
    ksk_lifetime: row.ksk_lifetime,
    ksk_algorithm: row.ksk_algorithm,
    zsk_lifetime: row.zsk_lifetime,
    zsk_algorithm: row.zsk_algorithm,
    purge_keys: row.purge_keys,
    signatures_validity: row.signatures_validity,
    signatures_validity_dnskey: row.signatures_validity_dnskey,
    signatures_refresh: row.signatures_refresh,
  })
  dialog.value = true
}

async function save() {
  saving.value = true
  try {
    if (editing.value) {
      await updateDNSSECPolicy(editing.value.id, form)
    } else {
      await createDNSSECPolicy(form)
    }
    dialog.value = false
    await load()
    toast.add({ title: 'Saved', color: 'success' })
  } catch (err) {
    toast.add({ title: errMsg(err, 'Failed to save DNSSEC policy'), color: 'error' })
  } finally {
    saving.value = false
  }
}

async function remove() {
  if (!editing.value) return
  if (!(await confirmDelete(`DNSSEC policy ${editing.value.name}`))) return
  deleting.value = true
  try {
    await deleteDNSSECPolicy(editing.value.id)
    dialog.value = false
    await load()
  } catch (err) {
    toast.add({ title: errMsg(err, 'Failed to delete DNSSEC policy'), color: 'error' })
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
        <div class="font-semibold text-lg">DNSSEC policies</div>
        <p class="text-muted-color text-sm">
          BIND dnssec-policy attached to a DNS template. factum2-dns writes the policy
          into the zone include on sync, and dnsmgr2 emits it in named.conf.
        </p>
      </div>
      <UButton
        v-if="authStore.canWrite"
        icon="i-lucide-plus"
        label="New policy"
        @click="openCreate"
      />
    </div>
    <SearchInput v-model="search" class="mb-3 max-w-xs" />
    <UTable
      :data="filtered"
      :columns="columns"
      :empty="search && items.length ? 'Nothing matches the search.' : 'No DNSSEC policies found.'"
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

  <FormModal
    v-model:open="dialog"
    :source="form"
    :title="editing ? 'Edit DNSSEC policy' : 'New DNSSEC policy'"
  >
    <template #body>
      <form id="dnssec-policy-form" class="space-y-3" @submit.prevent="save">
          <UFormField label="Name">
            <UInput v-model="form.name" class="w-full" required />
          </UFormField>
          <div class="grid grid-cols-2 gap-3">
            <UFormField label="KSK lifetime">
              <UInput v-model="form.ksk_lifetime" class="w-full" placeholder="P1y" />
            </UFormField>
            <UFormField label="KSK algorithm">
              <USelect v-model="form.ksk_algorithm" :items="algorithmItems" class="w-full" />
            </UFormField>
            <UFormField label="ZSK lifetime">
              <UInput v-model="form.zsk_lifetime" class="w-full" placeholder="30d" />
            </UFormField>
            <UFormField label="ZSK algorithm">
              <USelect v-model="form.zsk_algorithm" :items="algorithmItems" class="w-full" />
            </UFormField>
            <UFormField label="purge-keys">
              <UInput v-model="form.purge_keys" class="w-full" placeholder="365" />
            </UFormField>
            <UFormField label="signatures-validity">
              <UInput v-model="form.signatures_validity" class="w-full" placeholder="30d" />
            </UFormField>
            <UFormField label="signatures-validity-dnskey">
              <UInput v-model="form.signatures_validity_dnskey" class="w-full" placeholder="30d" />
            </UFormField>
            <UFormField label="signatures-refresh">
              <UInput v-model="form.signatures_refresh" class="w-full" placeholder="20d" />
            </UFormField>
          </div>
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
        <UButton type="submit" form="dnssec-policy-form" :loading="saving">Save</UButton>
      </div>
    </template>
  </FormModal>
</template>
