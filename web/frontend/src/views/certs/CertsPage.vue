<script setup>
import { computed, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useToast } from '@nuxt/ui/composables'
import { useAuthStore } from '@/stores/auth'
import { importLegoFile } from '@/api/certs'
import CertAccountsPage from './CertAccountsPage.vue'
import CertChallengesPage from './CertChallengesPage.vue'
import CertListPage from './CertListPage.vue'

defineOptions({ name: 'CertsPage' })

const route = useRoute()
const router = useRouter()
const toast = useToast()
const authStore = useAuthStore()

const tabs = [
  { label: 'Certificates', value: 'certificates', slot: 'certificates', icon: 'i-lucide-file-key' },
  { label: 'Accounts', value: 'accounts', slot: 'accounts', icon: 'i-lucide-user-cog' },
  { label: 'Challenges', value: 'challenges', slot: 'challenges', icon: 'i-lucide-shield' },
]

const tab = computed({
  get: () => (tabs.some((item) => item.value === route.query.tab) ? route.query.tab : 'certificates'),
  set: (value) =>
    router.replace({
      query: { ...route.query, tab: value === 'certificates' ? undefined : value },
    }),
})

const refresh = ref(0)
const importOpen = ref(false)
const importing = ref(false)
const importKey = ref(0)
const yamlFile = ref(null)
const envFile = ref(null)
const maxBytes = 1024 * 1024

function openImport() {
  yamlFile.value = null
  envFile.value = null
  importKey.value += 1
  importOpen.value = true
}

function onYamlFile(event) {
  yamlFile.value = event.target.files?.[0] ?? null
}

function onEnvFile(event) {
  envFile.value = event.target.files?.[0] ?? null
}

function readText(file) {
  return new Promise((resolve, reject) => {
    const reader = new FileReader()
    reader.onload = () => resolve(String(reader.result ?? ''))
    reader.onerror = () => reject(reader.error ?? new Error('Failed to read file'))
    reader.readAsText(file)
  })
}

function countLabel(created, updated, singular) {
  const parts = []
  if (created) parts.push(`${created} new`)
  if (updated) parts.push(`${updated} updated`)
  if (!parts.length) return ''
  const noun = created + updated === 1 ? singular : `${singular}s`
  return `${parts.join(', ')} ${noun}`
}

async function submitImport() {
  if (!yamlFile.value) {
    toast.add({ title: 'Choose a .lego.yaml file', color: 'error' })
    return
  }
  if (yamlFile.value.size > maxBytes || (envFile.value && envFile.value.size > maxBytes)) {
    toast.add({ title: 'File is too large', color: 'error' })
    return
  }
  importing.value = true
  try {
    const payload = { yaml: await readText(yamlFile.value), env: '' }
    if (envFile.value) payload.env = await readText(envFile.value)
    const result = await importLegoFile(payload)
    importOpen.value = false
    refresh.value += 1
    const summary = [
      countLabel(result.accounts_created, result.accounts_updated, 'account'),
      countLabel(result.challenges_created, result.challenges_updated, 'challenge'),
      countLabel(result.certificates_created, result.certificates_updated, 'certificate'),
    ]
      .filter(Boolean)
      .join('; ')
    toast.add({
      title: 'Imported .lego.yaml',
      description: summary || 'No rows changed',
      color: 'success',
    })
    if (result.skipped?.length) {
      toast.add({ title: 'Some entries were skipped', description: result.skipped.join(' '), color: 'warning' })
    }
  } catch (err) {
    toast.add({ title: err.response?.data?.error ?? 'Failed to import .lego.yaml', color: 'error' })
  } finally {
    importing.value = false
  }
}
</script>

<template>
  <div>
    <div v-if="authStore.canWrite" class="mb-3 flex justify-end">
      <UButton icon="i-lucide-upload" color="neutral" variant="outline" label="Import .lego.yaml" @click="openImport" />
    </div>
    <UTabs v-model="tab" :items="tabs" :unmount-on-hide="false">
      <template #certificates>
        <div class="pt-2">
          <CertListPage :active="tab === 'certificates'" :refresh="refresh" />
        </div>
      </template>
      <template #accounts>
        <div class="pt-2">
          <CertAccountsPage :active="tab === 'accounts'" :refresh="refresh" />
        </div>
      </template>
      <template #challenges>
        <div class="pt-2">
          <CertChallengesPage :active="tab === 'challenges'" :refresh="refresh" />
        </div>
      </template>
    </UTabs>

    <UModal v-model:open="importOpen" title="Import .lego.yaml" :dismissible="false">
      <template #body>
        <div :key="importKey" class="space-y-3">
          <p class="text-muted-color text-sm">
            Adds or updates accounts, challenges, and certificates by name. Rows that are not in
            the file are left as they are. Only RFC2136 DNS-01 challenges are imported.
          </p>
          <UFormField label=".lego.yaml" required>
            <input
              type="file"
              accept=".yaml,.yml,text/yaml"
              class="block w-full text-sm"
              @change="onYamlFile"
            />
          </UFormField>
          <UFormField
            label=".env (optional)"
            help="RFC2136 credentials. Applied to every imported challenge. Without this file, credentials already stored are left as they are."
          >
            <input type="file" accept=".env,text/plain" class="block w-full text-sm" @change="onEnvFile" />
          </UFormField>
        </div>
      </template>
      <template #footer>
        <div class="flex w-full gap-2">
          <UButton class="ms-auto" color="neutral" variant="ghost" type="button" @click="importOpen = false">
            Cancel
          </UButton>
          <UButton type="button" :loading="importing" :disabled="!yamlFile" @click="submitImport">Import</UButton>
        </div>
      </template>
    </UModal>
  </div>
</template>
