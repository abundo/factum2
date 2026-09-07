<script setup>
import { computed, onMounted, reactive, ref } from 'vue'
import { useToast } from '@nuxt/ui/composables'
import { useAuthStore } from '@/stores/auth'
import {
  createDnsTemplate,
  deleteDnsTemplate,
  listDNSSECPolicies,
  listDnsTemplates,
  listSOATemplates,
  updateDnsTemplate,
} from '@/api/dns'

defineOptions({ name: 'DnsTemplatesPage' })

const toast = useToast()
const authStore = useAuthStore()
const items = ref([])
const soas = ref([])
const policies = ref([])
const dialog = ref(false)
const saving = ref(false)
const editing = ref(null)
const form = reactive(emptyForm())

const columns = [
  { accessorKey: 'name', header: 'Name' },
  { accessorKey: 'soa_template', header: 'SOA' },
  { accessorKey: 'default_ttl', header: 'TTL' },
  { id: 'dnssec', accessorKey: 'dnssec_policy', header: 'DNSSEC' },
  { id: 'nameservers', header: 'NS' },
  { id: 'actions', header: '' },
]

const soaItems = computed(() => soas.value.map((s) => ({ label: s.name, value: s.id })))
const policyItems = computed(() => [
  { label: 'No DNSSEC', value: 0 },
  ...policies.value.map((p) => ({ label: p.name, value: p.id })),
])

function emptyForm() {
  return {
    name: '',
    soa_template_id: undefined,
    default_ttl: 3600,
    dnssec_policy_id: 0,
    nameservers: ['', ''],
  }
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
  const ns = [...(row.nameservers || [])]
  if (ns.length === 0) ns.push('')
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
      nameservers: form.nameservers.map((h) => h.trim()).filter(Boolean),
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

async function remove(row) {
  if (!confirm(`Delete DNS template ${row.name}?`)) return
  try {
    await deleteDnsTemplate(row.id)
    await load()
  } catch (err) {
    toast.add({ title: errMsg(err, 'Failed to delete DNS template'), color: 'error' })
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
          SOA, default TTL, nameservers and an optional DNSSEC policy.
        </p>
      </div>
      <UButton
        v-if="authStore.canWrite"
        icon="i-lucide-plus"
        label="New template"
        @click="openCreate"
      />
    </div>
    <UTable :data="items" :columns="columns">
      <template #dnssec-cell="{ row }">
        {{ row.original.dnssec_policy || '—' }}
      </template>
      <template #nameservers-cell="{ row }">
        {{ (row.original.nameservers || []).join(', ') }}
      </template>
      <template #actions-cell="{ row }">
        <div class="flex gap-2 justify-end">
          <UButton
            v-if="authStore.canWrite"
            size="xs"
            color="neutral"
            variant="ghost"
            icon="i-lucide-pencil"
            @click="openEdit(row.original)"
          />
          <UButton
            v-if="authStore.canWrite"
            size="xs"
            color="error"
            variant="ghost"
            icon="i-lucide-trash"
            @click="remove(row.original)"
          />
        </div>
      </template>
    </UTable>
  </div>

  <UModal v-model:open="dialog">
    <template #content>
      <UCard>
        <template #header>{{ editing ? 'Edit DNS template' : 'New DNS template' }}</template>
        <form class="space-y-3" @submit.prevent="save">
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
          <UFormField label="Nameservers (NS)">
            <div class="space-y-2">
              <div v-for="(host, i) in form.nameservers" :key="i" class="flex gap-2">
                <UInput
                  v-model="form.nameservers[i]"
                  class="w-full font-mono"
                  placeholder="ns1.example.com."
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
                @click="form.nameservers.push('')"
              >
                Add
              </UButton>
            </div>
          </UFormField>
          <div class="flex justify-end gap-2 pt-2">
            <UButton color="neutral" variant="ghost" type="button" @click="dialog = false"
              >Cancel</UButton
            >
            <UButton type="submit" :loading="saving">Save</UButton>
          </div>
        </form>
      </UCard>
    </template>
  </UModal>
</template>
