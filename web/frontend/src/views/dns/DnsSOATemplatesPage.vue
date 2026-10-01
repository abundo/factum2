<script setup>
import { onMounted, reactive, ref } from 'vue'
import { useToast } from '@nuxt/ui/composables'
import { useAuthStore } from '@/stores/auth'
import { useConfirm } from '@/composables/useConfirm'
import SearchInput from '@/components/SearchInput.vue'
import {
  createSOATemplate,
  deleteSOATemplate,
  listSOATemplates,
  updateSOATemplate,
} from '@/api/dns'
import { useSearch, valuesText } from '@/utils/search'

defineOptions({ name: 'DnsSOATemplatesPage' })

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
  { accessorKey: 'mname', header: 'MNAME' },
  { accessorKey: 'rname', header: 'RNAME' },
  { accessorKey: 'ttl', header: 'TTL' },
]
const { search, filtered } = useSearch(items, (row) =>
  valuesText(row.name, row.mname, row.rname, row.ttl),
)

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

async function remove() {
  if (!editing.value) return
  if (!(await confirmDelete(`SOA template ${editing.value.name}`))) return
  deleting.value = true
  try {
    await deleteSOATemplate(editing.value.id)
    dialog.value = false
    await load()
  } catch (err) {
    toast.add({ title: errMsg(err, 'Failed to delete SOA template'), color: 'error' })
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
    <SearchInput v-model="search" class="mb-3 max-w-xs" />
    <UTable
      :data="filtered"
      :columns="columns"
      :empty="search && items.length ? 'Nothing matches the search.' : 'No SOA templates found.'"
    >
      <template #actions-cell="{ row }">
        <UButton
          v-if="authStore.canWrite"
          size="sm"
          color="neutral"
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
    :title="editing ? 'Edit SOA template' : 'New SOA template'"
  >
    <template #body>
      <form id="soa-template-form" class="space-y-3" @submit.prevent="save">
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
        <UButton
          class="ms-auto"
          color="neutral"
          variant="ghost"
          type="button"
          @click="close"
          >Cancel</UButton
        >
        <UButton type="submit" form="soa-template-form" :loading="saving">Save</UButton>
      </div>
    </template>
  </FormModal>
</template>
