<script setup>
import { computed, onMounted, reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import { useToast } from '@nuxt/ui/composables'
import { useAuthStore } from '@/stores/auth'
import { createDnsZone, deleteDnsZone, listDnsTemplates, listDnsZones } from '@/api/dns'

defineOptions({ name: 'DnsZonesPage' })

const toast = useToast()
const router = useRouter()
const authStore = useAuthStore()
const zones = ref([])
const templates = ref([])
const loading = ref(true)
const dialog = ref(false)
const saving = ref(false)
const form = reactive(emptyForm())

const typeItems = [
  { label: 'Forward', value: 'forward' },
  { label: 'Reverse IPv4', value: 'reverse4' },
  { label: 'Reverse IPv6', value: 'reverse6' },
]

const columns = [
  { accessorKey: 'name', header: 'Name' },
  { accessorKey: 'type', header: 'Type' },
  { accessorKey: 'dns_template', header: 'DNS template' },
  { id: 'actions', header: '' },
]

const templateItems = computed(() => templates.value.map((s) => ({ label: s.name, value: s.id })))

function emptyForm() {
  return { name: '', type: 'forward', dns_template_id: undefined }
}

function typeLabel(type) {
  return typeItems.find((t) => t.value === type)?.label || type
}

function errMsg(err, fallback) {
  return err.response?.data?.error ?? fallback
}

async function load() {
  loading.value = true
  try {
    const [z, t] = await Promise.all([listDnsZones(), listDnsTemplates()])
    zones.value = z ?? []
    templates.value = t ?? []
  } catch (err) {
    toast.add({ title: errMsg(err, 'Failed to load zones'), color: 'error' })
  } finally {
    loading.value = false
  }
}

function openCreate() {
  if (!templates.value.length) return
  Object.assign(form, emptyForm())
  form.dns_template_id = templates.value[0]?.id
  dialog.value = true
}

async function save() {
  saving.value = true
  try {
    const created = await createDnsZone({
      name: form.name,
      type: form.type,
      dns_template_id: form.dns_template_id,
      records: [],
    })
    dialog.value = false
    router.push(`/dns/zones/${created.id}`)
  } catch (err) {
    toast.add({ title: errMsg(err, 'Failed to create zone'), color: 'error' })
  } finally {
    saving.value = false
  }
}

async function remove(row) {
  if (!confirm(`Delete zone ${row.name}?`)) return
  try {
    await deleteDnsZone(row.id)
    await load()
  } catch (err) {
    toast.add({ title: errMsg(err, 'Failed to delete zone'), color: 'error' })
  }
}

onMounted(load)
</script>

<template>
  <div class="card">
    <div class="flex items-center justify-between mb-4">
      <div>
        <div class="font-semibold text-lg">Zones</div>
        <p class="text-muted-color text-sm">
          Add, update and delete DNS zones. SOA and NS come from the DNS template.
        </p>
      </div>
      <UButton
        v-if="authStore.canWrite"
        icon="i-lucide-plus"
        label="New zone"
        :disabled="!templates.length"
        :title="templates.length ? undefined : 'Create a DNS template first'"
        @click="openCreate"
      />
    </div>
    <div v-if="loading" class="flex justify-center p-4">
      <UIcon name="i-lucide-loader-2" class="size-8 animate-spin" />
    </div>
    <UTable v-else :data="zones" :columns="columns">
      <template #name-cell="{ row }">
        <RouterLink class="text-primary font-medium" :to="`/dns/zones/${row.original.id}`">
          {{ row.original.name }}
        </RouterLink>
      </template>
      <template #type-cell="{ row }">
        {{ typeLabel(row.original.type) }}
      </template>
      <template #actions-cell="{ row }">
        <div class="flex gap-2 justify-end">
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
        <template #header>New zone</template>
        <form class="space-y-3" @submit.prevent="save">
          <UFormField label="Name">
            <UInput v-model="form.name" class="w-full" required placeholder="example.com" />
          </UFormField>
          <UFormField label="Type">
            <USelect v-model="form.type" :items="typeItems" class="w-full" />
          </UFormField>
          <UFormField
            label="DNS template"
            hint="NS and SOA records for the zone come from this template."
          >
            <USelect v-model="form.dns_template_id" :items="templateItems" class="w-full" />
          </UFormField>
          <div class="flex justify-end gap-2 pt-2">
            <UButton color="neutral" variant="ghost" type="button" @click="dialog = false"
              >Cancel</UButton
            >
            <UButton type="submit" :loading="saving">Create</UButton>
          </div>
        </form>
      </UCard>
    </template>
  </UModal>
</template>
