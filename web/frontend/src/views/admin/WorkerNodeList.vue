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

const toast = useToast()

const workerNodes = ref([])
const loading = ref(true)
const error = ref(null)
const forbidden = ref(false)

const nodeDialog = ref(false)
const node = ref({})
const submitted = ref(false)
const saving = ref(false)

const globalFilter = ref('')
const sorting = ref([{ id: 'name', desc: false }])

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

function hideDialog() {
  nodeDialog.value = false
  submitted.value = false
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
      <SearchInput v-model="globalFilter" />
    </div>

    <UTable
      v-model:sorting="sorting"
      v-model:global-filter="globalFilter"
      :data="workerNodes"
      :columns="columns"
      :loading="loading"
      :empty="error ?? 'No worker nodes found.'"
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

  <UModal v-model:open="nodeDialog" title="Worker Node Details" :ui="{ content: 'sm:max-w-lg' }">
    <template #body>
      <div class="flex flex-col gap-6">
        <div>
          <label for="name" class="block font-bold mb-3">Name</label>
          <UInput
            id="name"
            v-model.trim="node.name"
            :color="submitted && !node.name?.trim() ? 'error' : undefined"
            :highlight="submitted && !node.name?.trim()"
            autofocus
            class="w-full"
          />
          <small v-if="submitted && !node.name?.trim()" class="text-red-500"
            >Name is required.</small
          >
        </div>
        <div>
          <label for="address" class="block font-bold mb-3">Address</label>
          <UInput
            id="address"
            v-model.trim="node.address"
            :color="submitted && !node.address?.trim() ? 'error' : undefined"
            :highlight="submitted && !node.address?.trim()"
            placeholder="host:port"
            class="w-full"
          />
          <small v-if="submitted && !node.address?.trim()" class="text-red-500"
            >Address is required.</small
          >
          <small v-else class="text-muted-color"
            >Dialed as wss://host:port/hub. The hub certificate SAN must match this hostname or
            IP.</small
          >
        </div>
        <div>
          <label for="token" class="block font-bold mb-3">Token</label>
          <div class="flex gap-2">
            <PasswordInput
              id="token"
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
          <small v-if="submitted && !node.id && !node.token" class="text-red-500"
            >Token is required.</small
          >
          <small v-else-if="node.id" class="text-muted-color"
            >Leave blank to keep the current token.</small
          >
        </div>
        <div class="flex items-center gap-3">
          <USwitch v-model="node.tls_skip_verify" id="tls_skip_verify" />
          <label for="tls_skip_verify" class="font-bold">Skip TLS certificate verification</label>
        </div>
        <small v-if="node.tls_skip_verify" class="text-amber-600 -mt-3"
          >Encrypted, but a MITM with any certificate is accepted. Prefer pasting the worker's
          certificate as TLS CA instead.</small
        >
        <div v-if="!node.tls_skip_verify">
          <label for="tls_ca" class="block font-bold mb-3">TLS CA certificate (PEM)</label>
          <UTextarea
            id="tls_ca"
            v-model="node.tls_ca"
            :rows="6"
            placeholder="-----BEGIN CERTIFICATE-----"
            class="w-full font-mono text-sm"
          />
          <small class="text-muted-color"
            >Optional. Trust this CA (or the worker's self-signed hub.crt) instead of the system
            pool. Leave empty to use system CAs.</small
          >
        </div>
        <div class="flex items-center gap-3">
          <USwitch v-model="node.enabled" id="enabled" />
          <label for="enabled" class="font-bold">Enabled</label>
        </div>
      </div>
    </template>

    <template #footer>
      <UButton label="Cancel" icon="i-lucide-x" variant="ghost" @click="hideDialog" />
      <UButton label="Save" icon="i-lucide-check" :loading="saving" @click="saveWorkerNode" />
    </template>
  </UModal>
</template>
