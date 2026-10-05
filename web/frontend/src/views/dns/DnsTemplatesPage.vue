<script setup>
import { computed, onMounted, reactive, ref } from 'vue'
import { useToast } from '@nuxt/ui/composables'
import { useAuthStore } from '@/stores/auth'
import { useConfirm } from '@/composables/useConfirm'
import SearchInput from '@/components/SearchInput.vue'
import {
  createDnsTemplate,
  deleteDnsTemplate,
  listDNSSECPolicies,
  listDnsTemplates,
  listSOATemplates,
  updateDnsTemplate,
} from '@/api/dns'
import { useSearch, valuesText } from '@/utils/search'

defineOptions({ name: 'DnsTemplatesPage' })

const toast = useToast()
const authStore = useAuthStore()
const { confirmDelete } = useConfirm()
const items = ref([])
const soas = ref([])
const policies = ref([])
const dialog = ref(false)
const saving = ref(false)
const deleting = ref(false)
const editing = ref(null)
const form = reactive(emptyForm())

const columns = [
  { id: 'actions', header: '' },
  { accessorKey: 'name', header: 'Name' },
  { accessorKey: 'soa_template', header: 'SOA' },
  { accessorKey: 'default_ttl', header: 'TTL' },
  { id: 'dnssec', accessorKey: 'dnssec_policy', header: 'DNSSEC' },
  { id: 'nameservers', header: 'NS' },
]

const soaItems = computed(() => soas.value.map((s) => ({ label: s.name, value: s.id })))
const policyItems = computed(() => [
  { label: 'No DNSSEC', value: 0 },
  ...policies.value.map((p) => ({ label: p.name, value: p.id })),
])

function emptyNameserver() {
  return { hostname: '', address: '' }
}

function emptyForm() {
  return {
    name: '',
    soa_template_id: undefined,
    default_ttl: 3600,
    dnssec_policy_id: 0,
    nameservers: [emptyNameserver(), emptyNameserver()],
  }
}

const { search, filtered } = useSearch(items, (row) =>
  valuesText(
    row.name,
    row.soa_template,
    row.default_ttl,
    row.dnssec_policy,
    nameserverSummary(row.nameservers),
  ),
)

function nameserverSummary(list) {
  return (list || [])
    .map((ns) => {
      const host = (ns?.hostname || '').trim()
      const addr = (ns?.address || '').trim()
      if (!host) return ''
      return addr ? `${host} ${addr}` : host
    })
    .filter(Boolean)
    .join(', ')
}

function errMsg(err, fallback) {
  return err.response?.data?.error ?? fallback
}

async function load() {
  try {
    const [t, s, p] = await Promise.all([
      listDnsTemplates(),
      listSOATemplates(),
      listDNSSECPolicies(),
    ])
    items.value = t ?? []
    soas.value = s ?? []
    policies.value = p ?? []
  } catch (err) {
    toast.add({ title: errMsg(err, 'Failed to load DNS templates'), color: 'error' })
  }
}

function openCreate() {
  editing.value = null
  Object.assign(form, emptyForm())
  form.soa_template_id = soas.value[0]?.id
  dialog.value = true
}

function openEdit(row) {
  editing.value = row
  const ns = (row.nameservers || []).map((n) => ({
    hostname: n.hostname || '',
    address: n.address || '',
  }))
  if (ns.length === 0) ns.push(emptyNameserver())
  Object.assign(form, {
    name: row.name,
    soa_template_id: row.soa_template_id,
    default_ttl: row.default_ttl,
    dnssec_policy_id: row.dnssec_policy_id || 0,
    nameservers: ns,
  })
  dialog.value = true
}

async function save() {
  saving.value = true
  try {
    const payload = {
      name: form.name,
      soa_template_id: form.soa_template_id,
      default_ttl: form.default_ttl,
      dnssec_policy_id: form.dnssec_policy_id || null,
      nameservers: form.nameservers
        .map((n) => ({ hostname: n.hostname.trim(), address: n.address.trim() }))
        .filter((n) => n.hostname || n.address),
    }
    if (editing.value) {
      await updateDnsTemplate(editing.value.id, payload)
    } else {
      await createDnsTemplate(payload)
    }
    dialog.value = false
    await load()
    toast.add({ title: 'Saved', color: 'success' })
  } catch (err) {
    toast.add({ title: errMsg(err, 'Failed to save DNS template'), color: 'error' })
  } finally {
    saving.value = false
  }
}

async function remove() {
  if (!editing.value) return
  if (!(await confirmDelete(`DNS template ${editing.value.name}`))) return
  deleting.value = true
  try {
    await deleteDnsTemplate(editing.value.id)
    dialog.value = false
    await load()
  } catch (err) {
    toast.add({ title: errMsg(err, 'Failed to delete DNS template'), color: 'error' })
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
        <div class="font-semibold text-lg">DNS templates</div>
        <p class="text-muted-color text-sm">
          SOA, default TTL, nameservers with an optional address, and an optional DNSSEC policy.
        </p>
      </div>
      <UButton
        v-if="authStore.canWrite"
        icon="i-lucide-plus"
        label="New template"
        @click="openCreate"
      />
    </div>
    <SearchInput v-model="search" class="mb-3 max-w-xs" />
    <UTable
      :data="filtered"
      :columns="columns"
      :empty="search && items.length ? 'Nothing matches the search.' : 'No DNS templates found.'"
    >
      <template #dnssec-cell="{ row }">
        {{ row.original.dnssec_policy || '—' }}
      </template>
      <template #nameservers-cell="{ row }">
        {{ nameserverSummary(row.original.nameservers) }}
      </template>
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
    :title="editing ? 'Edit DNS template' : 'New DNS template'"
  >
    <template #body>
      <form id="dns-template-form" class="space-y-3" @submit.prevent="save">
          <UFormField label="Name">
            <UInput v-model="form.name" class="w-full" required />
          </UFormField>
          <UFormField label="SOA template">
            <USelect v-model="form.soa_template_id" :items="soaItems" class="w-full" />
          </UFormField>
          <UFormField label="Default TTL">
            <UInput v-model.number="form.default_ttl" type="number" class="w-full" />
          </UFormField>
          <UFormField label="DNSSEC policy">
            <USelect v-model="form.dnssec_policy_id" :items="policyItems" class="w-full" />
          </UFormField>
          <UFormField
            label="Nameservers (NS)"
            description="One IPv4 or IPv6 address per row. Add another row with the same name for another address."
          >
            <div class="space-y-2">
              <div
                class="hidden sm:grid grid-cols-[minmax(0,1fr)_minmax(0,1fr)_2rem] gap-2 text-xs text-muted-color"
              >
                <span>Name</span>
                <span>Address</span>
                <span></span>
              </div>
              <div
                v-for="(ns, i) in form.nameservers"
                :key="i"
                class="grid grid-cols-1 gap-2 sm:grid-cols-[minmax(0,1fr)_minmax(0,1fr)_auto]"
              >
                <UInput
                  v-model="form.nameservers[i].hostname"
                  class="w-full font-mono"
                  placeholder="ns1.example.com."
                  aria-label="Nameserver name"
                />
                <UInput
                  v-model="form.nameservers[i].address"
                  class="w-full font-mono"
                  placeholder="192.0.2.53"
                  aria-label="Nameserver address"
                />
                <UButton
                  type="button"
                  size="xs"
                  color="error"
                  variant="ghost"
                  icon="i-lucide-trash"
                  :disabled="form.nameservers.length < 2"
                  @click="form.nameservers.splice(i, 1)"
                />
              </div>
              <UButton
                type="button"
                size="xs"
                color="neutral"
                variant="outline"
                icon="i-lucide-plus"
                @click="form.nameservers.push(emptyNameserver())"
              >
                Add
              </UButton>
            </div>
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
        <UButton type="submit" form="dns-template-form" :loading="saving">Save</UButton>
      </div>
    </template>
  </FormModal>
</template>
