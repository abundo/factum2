<script setup>
import { onMounted, reactive, ref } from 'vue'
import { useToast } from '@nuxt/ui/composables'
import { useAuthStore } from '@/stores/auth'
import {
  createSOATemplate,
  deleteSOATemplate,
  listSOATemplates,
  updateSOATemplate,
} from '@/api/dns'

defineOptions({ name: 'DnsSOATemplatesPage' })

const toast = useToast()
const authStore = useAuthStore()
const items = ref([])
const dialog = ref(false)
const saving = ref(false)
const editing = ref(null)
const form = reactive(emptyForm())

const columns = [
  { accessorKey: 'name', header: 'Name' },
  { accessorKey: 'mname', header: 'MNAME' },
  { accessorKey: 'rname', header: 'RNAME' },
  { accessorKey: 'ttl', header: 'TTL' },
  { id: 'actions', header: '' },
]

function emptyForm() {
  return {
    name: '',
    mname: '',
    rname: '',
    refresh: 86400,
    retry: 7200,
    expire: 3600000,
    ttl: 3600,
  }
}

function errMsg(err, fallback) {
  return err.response?.data?.error ?? fallback
}

async function load() {
  try {
    items.value = (await listSOATemplates()) ?? []
  } catch (err) {
    toast.add({ title: errMsg(err, 'Failed to load SOA templates'), color: 'error' })
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
    mname: row.mname,
    rname: row.rname,
    refresh: row.refresh,
    retry: row.retry,
    expire: row.expire,
    ttl: row.ttl,
  })
  dialog.value = true
}

async function save() {
  saving.value = true
  try {
    if (editing.value) {
      await updateSOATemplate(editing.value.id, form)
    } else {
      await createSOATemplate(form)
    }
    dialog.value = false
    await load()
    toast.add({ title: 'Saved', color: 'success' })
  } catch (err) {
    toast.add({ title: errMsg(err, 'Failed to save SOA template'), color: 'error' })
  } finally {
    saving.value = false
  }
}

async function remove(row) {
  if (!confirm(`Delete SOA template ${row.name}?`)) return
  try {
    await deleteSOATemplate(row.id)
    await load()
  } catch (err) {
    toast.add({ title: errMsg(err, 'Failed to delete SOA template'), color: 'error' })
  }
}

onMounted(load)
</script>

<template>
  <div class="card">
    <div class="flex items-center justify-between mb-4">
      <div>
        <div class="font-semibold text-lg">SOA templates</div>
        <p class="text-muted-color text-sm">
          Used when dnsmgr2 writes zone files. Serial is calculated on sync.
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

  <FormModal v-model:open="dialog" :source="form">
    <template #content>
      <UCard>
        <template #header>{{ editing ? 'Edit SOA template' : 'New SOA template' }}</template>
        <form class="space-y-3" @submit.prevent="save">
          <UFormField label="Name">
            <UInput v-model="form.name" class="w-full" required />
          </UFormField>
          <div class="grid grid-cols-2 gap-3">
            <UFormField label="Primary nameserver (MNAME)">
              <UInput v-model="form.mname" class="w-full" placeholder="ns1.example.com." />
            </UFormField>
            <UFormField label="Email (RNAME)">
              <UInput v-model="form.rname" class="w-full" placeholder="hostmaster.example.com." />
            </UFormField>
          </div>
          <div class="grid grid-cols-2 gap-3 sm:grid-cols-4">
            <UFormField label="TTL">
              <UInput v-model.number="form.ttl" type="number" class="w-full" />
            </UFormField>
            <UFormField label="Refresh">
              <UInput v-model.number="form.refresh" type="number" class="w-full" />
            </UFormField>
            <UFormField label="Retry">
              <UInput v-model.number="form.retry" type="number" class="w-full" />
            </UFormField>
            <UFormField label="Expire">
              <UInput v-model.number="form.expire" type="number" class="w-full" />
            </UFormField>
          </div>
          <div class="flex justify-end gap-2 pt-2">
            <UButton color="neutral" variant="ghost" type="button" @click="dialog = false"
              >Cancel</UButton
            >
            <UButton type="submit" :loading="saving">Save</UButton>
          </div>
        </form>
      </UCard>
    </template>
  </FormModal>
</template>
