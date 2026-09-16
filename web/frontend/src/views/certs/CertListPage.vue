<script setup>
import { computed, onMounted, reactive, ref } from 'vue'
import { useToast } from '@nuxt/ui/composables'
import { useAuthStore } from '@/stores/auth'
import {
  createCertificate,
  deleteCertificate,
  listCertAccounts,
  listCertChallenges,
  listCertificates,
  updateCertificate,
} from '@/api/certs'

defineOptions({ name: 'CertListPage' })

const toast = useToast()
const authStore = useAuthStore()
const items = ref([])
const accounts = ref([])
const challenges = ref([])
const dialog = ref(false)
const saving = ref(false)
const editing = ref(null)
const form = reactive(emptyForm())

const columns = [
  { accessorKey: 'name', header: 'Name' },
  { id: 'domains', header: 'Domains' },
  { accessorKey: 'account', header: 'Account' },
  { accessorKey: 'challenge', header: 'Challenge' },
  { accessorKey: 'key_type', header: 'Key type' },
  { id: 'actions', header: '' },
]

const accountItems = computed(() => accounts.value.map((a) => ({ label: a.name, value: a.id })))
const challengeItems = computed(() => challenges.value.map((c) => ({ label: c.name, value: c.id })))
const keyTypeItems = [
  { label: 'Default', value: '' },
  { label: 'EC256', value: 'EC256' },
  { label: 'EC384', value: 'EC384' },
  { label: 'RSA2048', value: 'RSA2048' },
  { label: 'RSA4096', value: 'RSA4096' },
]
const cnItems = [
  { label: 'Default', value: 'default' },
  { label: 'On', value: 'on' },
  { label: 'Off', value: 'off' },
]

function emptyForm() {
  return {
    name: '',
    account_id: undefined,
    challenge_id: undefined,
    key_type: '',
    enable_cn: 'default',
    domains: [''],
  }
}

function errMsg(err, fallback) {
  return err.response?.data?.error ?? fallback
}

async function load() {
  try {
    const [certs, acc, ch] = await Promise.all([
      listCertificates(),
      listCertAccounts(),
      listCertChallenges(),
    ])
    items.value = certs ?? []
    accounts.value = acc ?? []
    challenges.value = ch ?? []
  } catch (err) {
    toast.add({ title: errMsg(err, 'Failed to load certificates'), color: 'error' })
  }
}

function openCreate() {
  editing.value = null
  Object.assign(form, emptyForm())
  form.account_id = accounts.value[0]?.id
  form.challenge_id = challenges.value[0]?.id
  dialog.value = true
}

function openEdit(row) {
  editing.value = row
  const domains = [...(row.domains || [])]
  if (domains.length === 0) domains.push('')
  let enable_cn = 'default'
  if (row.enable_common_name === true) enable_cn = 'on'
  if (row.enable_common_name === false) enable_cn = 'off'
  Object.assign(form, {
    name: row.name,
    account_id: row.account_id,
    challenge_id: row.challenge_id,
    key_type: row.key_type || '',
    enable_cn,
    domains,
  })
  dialog.value = true
}

async function save() {
  saving.value = true
  try {
    let enable_common_name = null
    if (form.enable_cn === 'on') enable_common_name = true
    if (form.enable_cn === 'off') enable_common_name = false
    const payload = {
      name: form.name,
      account_id: form.account_id,
      challenge_id: form.challenge_id,
      key_type: form.key_type || '',
      enable_common_name,
      domains: form.domains.map((d) => d.trim()).filter(Boolean),
    }
    if (editing.value) {
      await updateCertificate(editing.value.id, payload)
    } else {
      await createCertificate(payload)
    }
    dialog.value = false
    await load()
    toast.add({ title: 'Saved', color: 'success' })
  } catch (err) {
    toast.add({ title: errMsg(err, 'Failed to save certificate'), color: 'error' })
  } finally {
    saving.value = false
  }
}

async function remove(row) {
  if (!confirm(`Delete certificate ${row.name}?`)) return
  try {
    await deleteCertificate(row.id)
    await load()
  } catch (err) {
    toast.add({ title: errMsg(err, 'Failed to delete certificate'), color: 'error' })
  }
}

onMounted(load)
</script>

<template>
  <div class="card">
    <div class="flex items-center justify-between mb-4">
      <div>
        <div class="font-semibold text-lg">Certificates</div>
        <p class="text-muted-color text-sm">
          ACME certificates issued with lego (DNS-01 / RFC2136). Distribution is out of scope.
        </p>
      </div>
      <UButton
        v-if="authStore.canWrite"
        icon="i-lucide-plus"
        label="New certificate"
        @click="openCreate"
      />
    </div>
    <UTable :data="items" :columns="columns">
      <template #domains-cell="{ row }">
        {{ (row.original.domains || []).join(', ') }}
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

  <FormModal
    v-model:open="dialog"
    :source="form"
    :title="editing ? 'Edit certificate' : 'New certificate'"
  >
    <template #body>
      <form id="cert-form" class="space-y-3" @submit.prevent="save">
        <UFormField label="Name">
          <UInput v-model="form.name" class="w-full" required />
        </UFormField>
        <UFormField label="Account">
          <USelect v-model="form.account_id" :items="accountItems" class="w-full" />
        </UFormField>
        <UFormField label="Challenge">
          <USelect v-model="form.challenge_id" :items="challengeItems" class="w-full" />
        </UFormField>
        <UFormField label="Key type (override)">
          <USelect v-model="form.key_type" :items="keyTypeItems" class="w-full" />
        </UFormField>
        <UFormField label="Enable Common Name (override)">
          <USelect v-model="form.enable_cn" :items="cnItems" class="w-full" />
        </UFormField>
        <UFormField label="Domains">
          <div class="space-y-2">
            <div v-for="(d, i) in form.domains" :key="i" class="flex gap-2">
              <UInput
                v-model="form.domains[i]"
                class="w-full font-mono"
                placeholder="example.com or *.example.com"
              />
              <UButton
                type="button"
                size="xs"
                color="error"
                variant="ghost"
                icon="i-lucide-trash"
                :disabled="form.domains.length < 2"
                @click="form.domains.splice(i, 1)"
              />
            </div>
            <UButton
              type="button"
              size="xs"
              color="neutral"
              variant="outline"
              icon="i-lucide-plus"
              @click="form.domains.push('')"
            >
              Add
            </UButton>
          </div>
        </UFormField>
      </form>
    </template>
    <template #footer>
      <UButton color="neutral" variant="ghost" type="button" @click="dialog = false"
        >Cancel</UButton
      >
      <UButton type="submit" form="cert-form" :loading="saving">Save</UButton>
    </template>
  </FormModal>
</template>
