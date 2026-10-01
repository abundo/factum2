<script setup>
import { useToast } from '@nuxt/ui/composables'
import { onMounted, ref } from 'vue'
import {
  createWorkerNode,
  getWorkerNodeToken,
  getWorkerNodes,
  updateWorkerNode,
} from '@/api/workerNodes'
import PasswordInput from '@/components/PasswordInput.vue'
import SearchInput from '@/components/SearchInput.vue'
import SortableColumnHeader from '@/components/SortableColumnHeader.vue'
import { useSearch, valuesText } from '@/utils/search'

const toast = useToast()

const workerNodes = ref([])
const loading = ref(true)
const error = ref(null)
const forbidden = ref(false)

const nodeDialog = ref(false)
const node = ref({})
const submitted = ref(false)
const saving = ref(false)

const sorting = ref([{ id: 'name', desc: false }])
const { search, filtered } = useSearch(workerNodes, (row) =>
  valuesText(row.id, row.name, row.address, row.enabled ? 'Yes' : 'No'),
)

const columns = [
  { id: 'actions', header: '' },
  { accessorKey: 'id', header: 'ID' },
  { accessorKey: 'name', header: 'Name' },
  { accessorKey: 'address', header: 'Address' },
  { id: 'enabled', header: 'Enabled' },
]

function loadWorkerNodes() {
  loading.value = true
  error.value = null
  forbidden.value = false
  getWorkerNodes()
    .then((data) => {
      workerNodes.value = data ?? []
    })
    .catch((err) => {
      if (err.response?.status === 403 || err.response?.status === 401) {
        forbidden.value = true
      } else {
        error.value = 'Failed to load worker nodes.'
      }
    })
    .finally(() => {
      loading.value = false
    })
}

function openNew() {
  node.value = { enabled: true, tls_skip_verify: false, tls_ca: '' }
  submitted.value = false
  nodeDialog.value = true
}

// The token field is prefilled with the node's actual current token (masked
// by the Password input's own built-in eye icon, same as a freshly typed
// one) so that icon alone is enough to reveal it - no separate "show
// current" control. If the fetch fails, the field just stays blank, which
// already means "leave unchanged" on save (see saveWorkerNode).
function editWorkerNode(row) {
  node.value = {
    ...row,
    token: '',
    tls_skip_verify: !!row.tls_skip_verify,
    tls_ca: row.tls_ca ?? '',
  }
  submitted.value = false
  nodeDialog.value = true
  getWorkerNodeToken(row.id)
    .then((data) => {
      node.value.token = data.token
    })
    .catch(() => {
      // Leave blank - saving without changing it still preserves the
      // existing token, and Generate is right there if a fresh one is
      // wanted instead.
    })
}

function generateToken() {
  const bytes = new Uint8Array(32)
  crypto.getRandomValues(bytes)
  node.value.token = Array.from(bytes, (b) => b.toString(16).padStart(2, '0')).join('')
}

function saveWorkerNode() {
  submitted.value = true

  if (
    !node.value.name?.trim() ||
    !node.value.address?.trim() ||
    (!node.value.id && !node.value.token)
  ) {
    return
  }

  saving.value = true
  const payload = { ...node.value }
  if (!payload.token) {
    delete payload.token
  }
  const request = payload.id ? updateWorkerNode(payload.id, payload) : createWorkerNode(payload)

  request
    .then(() => {
      toast.add({
        color: 'success',
        title: 'Successful',
        description: payload.id ? 'Worker node updated' : 'Worker node created',
        duration: 3000,
      })
      nodeDialog.value = false
      loadWorkerNodes()
    })
    .catch((err) => {
      toast.add({
        color: 'error',
        title: 'Error',
        description: err.response?.data?.error ?? 'Failed to save worker node.',
        duration: 3000,
      })
    })
    .finally(() => {
      saving.value = false
    })
}

onMounted(loadWorkerNodes)
</script>

<template>
  <div v-if="forbidden" class="card">
    <UAlert
      color="error"
      variant="subtle"
      title="You need administrator permissions to view worker nodes."
    />
  </div>
  <div v-else class="card">
    <div class="flex flex-wrap gap-2 items-center justify-between mb-4">
      <div class="flex items-center gap-2">
        <h4 class="m-0">Worker nodes</h4>
        <UButton label="New" icon="i-lucide-plus" color="neutral" size="sm" @click="openNew" />
      </div>
      <SearchInput v-model="search" />
    </div>

    <UTable
      v-model:sorting="sorting"
      :data="filtered"
      :columns="columns"
      :loading="loading"
      :empty="
        error || (search && workerNodes.length ? 'Nothing matches the search.' : 'No worker nodes found.')
      "
      :virtualize="{ estimateSize: 46 }"
      class="max-h-[calc(100vh-380px)]"
    >
      <template
        v-for="col in columns.filter((c) => c.accessorKey)"
        :key="col.accessorKey"
        #[`${col.accessorKey}-header`]="{ column }"
      >
        <SortableColumnHeader :column="column" :label="col.header" />
      </template>

      <template #actions-cell="{ row }">
        <UButton
          icon="i-lucide-pencil"
          variant="outline"
          color="neutral"
          size="sm"
          @click="editWorkerNode(row.original)"
        />
      </template>
      <template #enabled-cell="{ row }">
        <UBadge
          :label="row.original.enabled ? 'Yes' : 'No'"
          :color="row.original.enabled ? 'success' : 'neutral'"
          variant="subtle"
        />
      </template>
    </UTable>
  </div>

  <FormModal v-model:open="nodeDialog" :source="node" title="Worker Node Details">
    <template #body>
      <div class="flex flex-col gap-4">
        <UFormField label="Name">
          <UInput
            v-model.trim="node.name"
            :color="submitted && !node.name?.trim() ? 'error' : undefined"
            :highlight="submitted && !node.name?.trim()"
            autofocus
            class="w-full"
          />
          <small v-if="submitted && !node.name?.trim()" class="text-red-500">Name is required.</small>
        </UFormField>
        <UFormField label="Address" hint="Dialed as wss://host:port/hub. The hub certificate SAN must match this hostname or IP.">
          <UInput
            v-model.trim="node.address"
            :color="submitted && !node.address?.trim() ? 'error' : undefined"
            :highlight="submitted && !node.address?.trim()"
            placeholder="host:port"
            class="w-full"
          />
          <small v-if="submitted && !node.address?.trim()" class="text-red-500">Address is required.</small>
        </UFormField>
        <UFormField label="Token">
          <div class="flex gap-2">
            <PasswordInput
              v-model="node.token"
              :color="submitted && !node.id && !node.token ? 'error' : undefined"
              :highlight="submitted && !node.id && !node.token"
              class="flex-1"
            />
            <UButton
              v-if="!node.token"
              label="Generate"
              icon="i-lucide-refresh-cw"
              type="button"
              color="neutral"
              variant="outline"
              @click="generateToken"
            />
          </div>
          <small v-if="submitted && !node.id && !node.token" class="text-red-500">Token is required.</small>
          <small v-else-if="node.id" class="text-muted-color">Leave blank to keep the current token.</small>
        </UFormField>
        <UFormField label="Skip TLS certificate verification">
          <USwitch v-model="node.tls_skip_verify" />
          <small v-if="node.tls_skip_verify" class="text-amber-600"
            >Encrypted, but a MITM with any certificate is accepted. Prefer pasting the worker's
            certificate as TLS CA instead.</small
          >
        </UFormField>
        <UFormField
          v-if="!node.tls_skip_verify"
          label="TLS CA certificate (PEM)"
          hint="Optional. Trust this CA (or the worker's self-signed hub.crt) instead of the system pool. Leave empty to use system CAs."
        >
          <UTextarea
            v-model="node.tls_ca"
            :rows="6"
            placeholder="-----BEGIN CERTIFICATE-----"
            class="w-full font-mono text-sm"
          />
        </UFormField>
        <UFormField label="Enabled">
          <USwitch v-model="node.enabled" />
        </UFormField>
      </div>
    </template>

    <template #footer="{ close }">
      <div class="flex w-full gap-2">
        <UButton class="ms-auto" label="Cancel" icon="i-lucide-x" variant="ghost" @click="close" />
        <UButton label="Save" icon="i-lucide-check" :loading="saving" @click="saveWorkerNode" />
      </div>
    </template>
  </FormModal>
</template>
