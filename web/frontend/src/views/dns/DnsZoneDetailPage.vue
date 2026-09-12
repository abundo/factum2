<script setup>
import { computed, onMounted, reactive, ref } from 'vue'
import { useRoute } from 'vue-router'
import { useToast } from '@nuxt/ui/composables'
import { useAuthStore } from '@/stores/auth'
import ZoneRecordsTable from '@/components/ZoneRecordsTable.vue'
import { getDnsZone, listDnsTemplates, listSOATemplates, updateDnsZone } from '@/api/dns'
import { fromApiRecord, toApiRecords, validateZoneRecords } from '@/utils/zoneRecords'
import { formatZoneFile, parseZoneFile } from '@/utils/zoneFile'

defineOptions({ name: 'DnsZoneDetailPage' })

const toast = useToast()
const route = useRoute()
const authStore = useAuthStore()
const loaded = ref(false)
const saving = ref(false)
const importInput = ref(null)
const templates = ref([])
const soas = ref([])
const form = reactive({
  name: '',
  type: 'forward',
  dns_template_id: undefined,
  records: [],
  comment: '',
})

const canWrite = computed(() => authStore.canWrite)
const activeTab = ref('records')
const templateItems = computed(() => templates.value.map((s) => ({ label: s.name, value: s.id })))
const selectedTemplate = computed(() =>
  templates.value.find((item) => Number(item.id) === Number(form.dns_template_id)),
)
const selectedSoa = computed(() => {
  const tmpl = selectedTemplate.value
  if (!tmpl) return null
  return soas.value.find((s) => Number(s.id) === Number(tmpl.soa_template_id)) || null
})
const selectedNameservers = computed(() => selectedTemplate.value?.nameservers || [])

const typeItems = [
  { label: 'Forward', value: 'forward' },
  { label: 'Reverse IPv4', value: 'reverse4' },
  { label: 'Reverse IPv6', value: 'reverse6' },
]
const typeLabel = computed(
  () => typeItems.find((item) => item.value === form.type)?.label || form.type,
)
const tabItems = [
  { label: 'Zone info', value: 'info', slot: 'info' },
  { label: 'SOA', value: 'soa', slot: 'soa' },
  { label: 'Records', value: 'records', slot: 'records' },
]

function errMsg(err, fallback) {
  return err.response?.data?.error ?? fallback
}

function recordT(key, params) {
  const strings = {
    'zoneRecords.nameRequired': 'Name is required',
    'zoneRecords.typeRequired': 'Type is required',
    'zoneRecords.unknownType': 'Unknown type {type}',
    'zoneRecords.valueRequired': 'Value is required',
    'zoneRecords.aMustBeIpv4': 'A record must be an IPv4 address',
    'zoneRecords.aaaaMustBeIpv6': 'AAAA record must be an IPv6 address',
    'zoneRecords.macOnlyA': 'MAC is only valid on A and AAAA records',
    'zoneRecords.macInvalid': 'MAC must be 12 hex digits (aa:bb:cc:dd:ee:ff)',
    'zoneRecords.recordN': 'Record {n}: {message}',
  }
  let s = strings[key] ?? key
  if (params) {
    for (const [k, v] of Object.entries(params)) {
      s = s.replaceAll(`{${k}}`, String(v))
    }
  }
  return s
}

onMounted(async () => {
  try {
    const [z, tpls, s] = await Promise.all([
      getDnsZone(route.params.id),
      listDnsTemplates(),
      listSOATemplates(),
    ])
    templates.value = tpls ?? []
    soas.value = s ?? []
    form.name = z.name
    form.type = z.type || 'forward'
    form.dns_template_id = z.dns_template_id
    form.records = (z.records || []).map(fromApiRecord)
    form.comment = z.comment || ''
    loaded.value = true
  } catch (err) {
    toast.add({ title: errMsg(err, 'Failed to load zone'), color: 'error' })
  }
})

function openImport() {
  importInput.value?.click()
}

function exportZoneFile() {
  const text = formatZoneFile({
    origin: form.name,
    soa: selectedSoa.value,
    defaultTtl: selectedTemplate.value?.default_ttl,
    nameservers: selectedNameservers.value,
    records: toApiRecords(form.records),
    comment: form.comment,
  })
  const blob = new Blob([text], { type: 'text/plain;charset=utf-8' })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = `${form.name.replace(/[^\w.-]+/g, '_')}.zone`
  a.click()
  URL.revokeObjectURL(url)
}

async function onImportFile(event) {
  const file = event.target.files?.[0]
  event.target.value = ''
  if (!file) return
  try {
    const text = await file.text()
    const parsed = parseZoneFile(text, form.name)
    form.records = parsed.records
    const skipped = []
    if (parsed.skippedSoa) skipped.push('SOA')
    if (parsed.skippedApexNs) skipped.push('NS for @')
    if (parsed.skippedUnknown) skipped.push('unknown types')
    toast.add({
      title: `Imported ${parsed.records.length} records`,
      description: skipped.length
        ? `${skipped.join(', ')} were skipped. Save to apply.`
        : 'Save to apply.',
      color: 'success',
    })
  } catch (err) {
    toast.add({ title: err.message || 'Could not import the file', color: 'error' })
  }
}

async function save() {
  if (!canWrite.value) return
  const validation = validateZoneRecords(form.records, recordT)
  if (validation) {
    toast.add({ title: validation, color: 'error' })
    return
  }
  saving.value = true
  try {
    const data = await updateDnsZone(route.params.id, {
      name: form.name,
      type: form.type,
      dns_template_id: form.dns_template_id,
      comment: form.comment,
      records: toApiRecords(form.records),
    })
    form.records = (data.records || []).map(fromApiRecord)
    form.dns_template_id = data.dns_template_id
    form.comment = data.comment || ''
    form.type = data.type || form.type
    toast.add({ title: 'Zone saved', color: 'success' })
  } catch (err) {
    toast.add({ title: errMsg(err, 'Failed to save zone'), color: 'error' })
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <div v-if="loaded">
    <div class="flex items-center gap-2 mb-4">
      <UButton to="/dns/zones" color="neutral" variant="ghost" icon="i-lucide-arrow-left" />
      <div class="font-semibold text-lg">{{ form.name }}</div>
      <UBadge color="neutral" variant="subtle">{{ typeLabel }}</UBadge>
    </div>
    <UCard class="w-full min-w-0">
      <form @submit.prevent="save">
        <UTabs v-model="activeTab" :items="tabItems">
          <template #info>
            <div class="space-y-4 pt-4">
              <div v-if="canWrite" class="flex">
                <UButton type="submit" :loading="saving">Save</UButton>
              </div>
              <div class="grid gap-x-4 gap-y-3 sm:grid-cols-2">
                <UFormField label="Name">
                  <UInput v-model="form.name" class="w-full" :disabled="!canWrite" />
                </UFormField>
                <UFormField
                  label="DNS template"
                  hint="NS and SOA records come from this template."
                  class="sm:col-span-2"
                >
                  <USelect
                    v-model="form.dns_template_id"
                    :items="templateItems"
                    class="w-full"
                    :disabled="!canWrite"
                  />
                </UFormField>
              </div>
              <UFormField label="Comments" description="Operator notes. Not part of the DNS RDATA.">
                <UTextarea
                  v-model="form.comment"
                  class="w-full font-mono"
                  :rows="8"
                  :disabled="!canWrite"
                  placeholder="Notes for this zone…"
                />
              </UFormField>
            </div>
          </template>
          <template #soa>
            <div class="space-y-4 pt-4">
              <p class="text-muted-color text-sm">Values come from the selected DNS template.</p>
              <template v-if="selectedSoa">
                <div class="grid grid-cols-2 gap-x-4 gap-y-3">
                  <UFormField label="Primary nameserver (MNAME)">
                    <UInput :model-value="selectedSoa.mname" disabled class="w-full" />
                  </UFormField>
                  <UFormField label="Email (RNAME)">
                    <UInput :model-value="selectedSoa.rname" disabled class="w-full" />
                  </UFormField>
                </div>
                <div class="grid grid-cols-2 gap-x-4 gap-y-3 sm:grid-cols-5">
                  <UFormField label="Serial">
                    <UInput model-value="Calculated" disabled class="w-full" />
                  </UFormField>
                  <UFormField label="TTL">
                    <UInput :model-value="selectedSoa.ttl" disabled class="w-full" />
                  </UFormField>
                  <UFormField label="Refresh">
                    <UInput :model-value="selectedSoa.refresh" disabled class="w-full" />
                  </UFormField>
                  <UFormField label="Retry">
                    <UInput :model-value="selectedSoa.retry" disabled class="w-full" />
                  </UFormField>
                  <UFormField label="Expire">
                    <UInput :model-value="selectedSoa.expire" disabled class="w-full" />
                  </UFormField>
                </div>
                <div>
                  <div class="text-sm font-medium mb-1">Nameservers</div>
                  <ul v-if="selectedNameservers.length" class="text-sm font-mono">
                    <li v-for="ns in selectedNameservers" :key="ns">{{ ns }}</li>
                  </ul>
                  <p v-else class="text-muted-color text-sm">—</p>
                </div>
              </template>
              <p v-else class="text-muted-color text-sm">Select a DNS template to see SOA values.</p>
            </div>
          </template>
          <template #records>
            <div class="pt-4">
              <p class="text-muted-color text-sm mb-3">
                Leave TTL empty to use the template default. SOA and apex NS come from the DNS
                template and are skipped on import.
              </p>
              <ZoneRecordsTable v-model="form.records" :disabled="!canWrite">
                <template #leading-actions>
                  <UButton v-if="canWrite" type="submit" :loading="saving">Save</UButton>
                </template>
                <template #actions>
                  <UButton
                    type="button"
                    color="neutral"
                    variant="outline"
                    icon="i-lucide-download"
                    title="Download as a BIND zone file"
                    @click="exportZoneFile"
                  >
                    Export
                  </UButton>
                  <UButton
                    v-if="canWrite"
                    type="button"
                    color="neutral"
                    variant="outline"
                    icon="i-lucide-upload"
                    title="Replaces all records. SOA and NS for @ are ignored."
                    @click="openImport"
                  >
                    Import
                  </UButton>
                </template>
              </ZoneRecordsTable>
            </div>
          </template>
        </UTabs>
      </form>
    </UCard>
    <input
      ref="importInput"
      type="file"
      accept=".txt,.zone,text/plain"
      class="hidden"
      @change="onImportFile"
    />
  </div>
</template>
