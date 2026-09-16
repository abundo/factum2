<script setup>
import { onMounted, reactive, ref } from 'vue'
import { useToast } from '@nuxt/ui/composables'
import { useAuthStore } from '@/stores/auth'
import PasswordInput from '@/components/PasswordInput.vue'
import {
  createCertChallenge,
  deleteCertChallenge,
  listCertChallenges,
  updateCertChallenge,
} from '@/api/certs'

defineOptions({ name: 'CertChallengesPage' })

const toast = useToast()
const authStore = useAuthStore()
const items = ref([])
const dialog = ref(false)
const saving = ref(false)
const editing = ref(null)
const form = reactive(emptyForm())

const columns = [
  { accessorKey: 'name', header: 'Name' },
  { accessorKey: 'kind', header: 'Kind' },
  { accessorKey: 'provider', header: 'Provider' },
  { accessorKey: 'rfc2136_nameserver', header: 'Nameserver' },
  { id: 'actions', header: '' },
]

function emptyForm() {
  return {
    name: '',
    kind: 'dns-01',
    provider: 'rfc2136',
    dns_timeout: 10,
    resolvers: '',
    disable_authoritative_nameservers: false,
    disable_recursive_nameservers: false,
    propagation_wait: '',
    rfc2136_nameserver: '',
    rfc2136_tsig_algorithm: 'hmac-sha256.',
    rfc2136_tsig_key: '',
    rfc2136_tsig_secret: '',
    rfc2136_tsig_file: '',
    rfc2136_ttl: 120,
    rfc2136_propagation_timeout: '',
    rfc2136_polling_interval: '',
    extra_env: '',
  }
}

function errMsg(err, fallback) {
  return err.response?.data?.error ?? fallback
}

async function load() {
  try {
    items.value = (await listCertChallenges()) ?? []
  } catch (err) {
    toast.add({ title: errMsg(err, 'Failed to load challenges'), color: 'error' })
  }
}

function openCreate() {
  editing.value = null
  Object.assign(form, emptyForm())
  dialog.value = true
}

function openEdit(row) {
  editing.value = row
  Object.assign(form, emptyForm(), row)
  dialog.value = true
}

async function save() {
  saving.value = true
  try {
    const payload = { ...form }
    if (editing.value) {
      await updateCertChallenge(editing.value.id, payload)
    } else {
      await createCertChallenge(payload)
    }
    dialog.value = false
    await load()
    toast.add({ title: 'Saved', color: 'success' })
  } catch (err) {
    toast.add({ title: errMsg(err, 'Failed to save challenge'), color: 'error' })
  } finally {
    saving.value = false
  }
}

async function remove(row) {
  if (!confirm(`Delete challenge ${row.name}?`)) return
  try {
    await deleteCertChallenge(row.id)
    await load()
  } catch (err) {
    toast.add({ title: errMsg(err, 'Failed to delete challenge'), color: 'error' })
  }
}

onMounted(load)
</script>

<template>
  <div class="card">
    <div class="flex items-center justify-between mb-4">
      <div>
        <div class="font-semibold text-lg">DNS-01 challenges</div>
        <p class="text-muted-color text-sm">
          RFC2136 dynamic updates for Let’s Encrypt DNS-01. Credentials go into the lego .env on
          sync.
        </p>
      </div>
      <UButton
        v-if="authStore.canWrite"
        icon="i-lucide-plus"
        label="New challenge"
        @click="openCreate"
      />
    </div>
    <UTable :data="items" :columns="columns">
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

  <FormModal
    v-model:open="dialog"
    :source="form"
    :title="editing ? 'Edit challenge' : 'New challenge'"
  >
    <template #body>
      <form id="cert-challenge-form" class="space-y-3" @submit.prevent="save">
        <UFormField label="Name">
          <UInput v-model="form.name" class="w-full" required />
        </UFormField>
        <UFormField label="Kind">
          <UInput v-model="form.kind" class="w-full" disabled />
        </UFormField>
        <UFormField label="Provider">
          <UInput v-model="form.provider" class="w-full" disabled />
        </UFormField>
        <UFormField label="RFC2136 nameserver">
          <UInput
            v-model="form.rfc2136_nameserver"
            class="w-full font-mono"
            placeholder="ns.example.com:53"
          />
        </UFormField>
        <UFormField label="TSIG algorithm">
          <UInput v-model="form.rfc2136_tsig_algorithm" class="w-full font-mono" />
        </UFormField>
        <UFormField label="TSIG key">
          <UInput v-model="form.rfc2136_tsig_key" class="w-full" />
        </UFormField>
        <UFormField label="TSIG secret">
          <PasswordInput v-model="form.rfc2136_tsig_secret" />
        </UFormField>
        <UFormField label="TSIG key file (optional)">
          <UInput v-model="form.rfc2136_tsig_file" class="w-full" />
        </UFormField>
        <UFormField label="TXT TTL">
          <UInput v-model.number="form.rfc2136_ttl" type="number" class="w-full" />
        </UFormField>
        <UFormField label="Resolvers (one host:port per line)">
          <UTextarea v-model="form.resolvers" :rows="2" class="w-full font-mono" />
        </UFormField>
        <UFormField label="DNS timeout (seconds)">
          <UInput v-model.number="form.dns_timeout" type="number" class="w-full" />
        </UFormField>
        <div class="flex items-center gap-2">
          <USwitch v-model="form.disable_authoritative_nameservers" id="noauth" />
          <label for="noauth">Skip authoritative NS propagation check</label>
        </div>
        <div class="flex items-center gap-2">
          <USwitch v-model="form.disable_recursive_nameservers" id="norec" />
          <label for="norec">Skip recursive resolver propagation check</label>
        </div>
        <UFormField label="Extra env (KEY=value per line)">
          <UTextarea v-model="form.extra_env" :rows="3" class="w-full font-mono" />
        </UFormField>
      </form>
    </template>
    <template #footer>
      <UButton color="neutral" variant="ghost" type="button" @click="dialog = false"
        >Cancel</UButton
      >
      <UButton type="submit" form="cert-challenge-form" :loading="saving">Save</UButton>
    </template>
  </FormModal>
</template>
