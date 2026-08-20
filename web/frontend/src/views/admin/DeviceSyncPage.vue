<script setup>
import { useToast } from '@nuxt/ui/composables'
import { onMounted, ref } from 'vue'
import {
  createDeviceSyncAuth,
  deleteDeviceSyncAuth,
  getDeviceSyncAuthPassword,
  getDeviceSyncAuths,
  updateDeviceSyncAuth,
} from '@/api/deviceSyncAuth'
import PasswordInput from '@/components/PasswordInput.vue'
import SortableColumnHeader from '@/components/SortableColumnHeader.vue'
import { useSettings } from '@/composables/useSettings'

const toast = useToast()

const {
  settings,
  loading: settingsLoading,
  saving: settingsSaving,
  save: saveSettings,
} = useSettings()

const auths = ref([])
const loading = ref(true)
const error = ref(null)
const forbidden = ref(false)

const authDialog = ref(false)
const auth = ref({})
const submitted = ref(false)
const saving = ref(false)

const deleteDialog = ref(false)
const authToDelete = ref(null)

const globalFilter = ref('')
const sorting = ref([{ id: 'name', desc: false }])

const columns = [
  { id: 'actions', header: '' },
  { accessorKey: 'id', header: 'ID' },
  { accessorKey: 'name', header: 'Name' },
  { accessorKey: 'username', header: 'Username' },
]

function loadDeviceSyncAuths() {
  loading.value = true
  error.value = null
  forbidden.value = false
  getDeviceSyncAuths()
    .then((data) => {
      auths.value = data ?? []
    })
    .catch((err) => {
      if (err.response?.status === 403 || err.response?.status === 401) {
        forbidden.value = true
      } else {
        error.value = 'Failed to load device sync credentials.'
      }
    })
    .finally(() => {
      loading.value = false
    })
}

function openNew() {
  auth.value = {}
  submitted.value = false
  authDialog.value = true
}

// The password field is prefilled with the row's actual current password
// (masked by the Password input's own built-in eye icon, same as a freshly
// typed one) so that icon alone is enough to reveal it - no separate "show
// current" control. If the fetch fails, the field just stays blank, which
// already means "leave unchanged" on save (see saveDeviceSyncAuth).
function editDeviceSyncAuth(row) {
  auth.value = { ...row, password: '' }
  submitted.value = false
  authDialog.value = true
  getDeviceSyncAuthPassword(row.id)
    .then((data) => {
      auth.value.password = data.password
    })
    .catch(() => {
      // Leave blank - saving without changing it still preserves the
      // existing password.
    })
}

function hideDialog() {
  authDialog.value = false
  submitted.value = false
}

function saveDeviceSyncAuth() {
  submitted.value = true

  if (!auth.value.name?.trim()) {
    return
  }

  saving.value = true
  const payload = { ...auth.value }
  if (!payload.password) {
    delete payload.password
  }
  const request = payload.id
    ? updateDeviceSyncAuth(payload.id, payload)
    : createDeviceSyncAuth(payload)

  request
    .then(() => {
      toast.add({
        color: 'success',
        title: 'Successful',
        description: payload.id ? 'Credentials updated' : 'Credentials created',
        duration: 3000,
      })
      authDialog.value = false
      loadDeviceSyncAuths()
    })
    .catch((err) => {
      toast.add({
        color: 'error',
        title: 'Error',
        description: err.response?.data?.error ?? 'Failed to save credentials.',
        duration: 3000,
      })
    })
    .finally(() => {
      saving.value = false
    })
}

function confirmDelete(row) {
  authToDelete.value = row
  deleteDialog.value = true
}

function performDelete() {
  deleteDeviceSyncAuth(authToDelete.value.id)
    .then(() => {
      toast.add({
        color: 'success',
        title: 'Successful',
        description: 'Credentials deleted',
        duration: 3000,
      })
      loadDeviceSyncAuths()
    })
    .catch((err) => {
      toast.add({
        color: 'error',
        title: 'Error',
        description: err.response?.data?.error ?? 'Failed to delete credentials.',
        duration: 3000,
      })
    })
    .finally(() => {
      deleteDialog.value = false
      authToDelete.value = null
    })
}

onMounted(loadDeviceSyncAuths)
</script>

<template>
  <div v-if="forbidden" class="card">
    <UAlert
      color="error"
      variant="subtle"
      title="You need administrator permissions to view device sync."
    />
  </div>
  <template v-else>
    <div class="card mb-6">
      <div class="flex items-center justify-between mb-4">
        <div class="font-semibold text-xl">Device sync</div>
        <UButton
          label="Save"
          icon="i-lucide-check"
          :loading="settingsSaving"
          :disabled="settingsLoading"
          @click="saveSettings"
        />
      </div>

      <div v-if="settingsLoading" class="flex justify-center p-4">
        <UIcon name="i-lucide-loader-2" class="size-8 animate-spin" />
      </div>

      <div v-else class="flex flex-col gap-6">
        <div class="flex items-center gap-2">
          <USwitch v-model="settings.device_sync_enabled" id="device_sync_enabled" />
          <label for="device_sync_enabled" class="font-bold">Enabled</label>
        </div>
        <p class="text-muted-color text-sm -mt-3">
          Syncs device interfaces/addresses/connections into Netbox. Netbox connection settings
          are shared with the Netbox tab under Sources settings; per-device login credentials
          are managed below.
        </p>
        <div>
          <label for="device_sync_vrf_in_global" class="block font-bold mb-3"
            >VRFs allocated in the global table</label
          >
          <UTextarea
            id="device_sync_vrf_in_global"
            v-model="settings.device_sync_vrf_in_global"
            :rows="4"
            placeholder="One VRF name per line"
            class="w-full"
          />
        </div>
        <div>
          <label for="device_sync_device_states" class="block font-bold mb-3"
            >Netbox device states to sync</label
          >
          <UTextarea
            id="device_sync_device_states"
            v-model="settings.device_sync_device_states"
            :rows="4"
            placeholder="One state per line, e.g. Active"
            class="w-full"
          />
        </div>
        <div>
          <label for="device_sync_device_ignore" class="block font-bold mb-3">Ignore devices</label>
          <UTextarea
            id="device_sync_device_ignore"
            v-model="settings.device_sync_device_ignore"
            :rows="4"
            placeholder="One device name per line"
            class="w-full"
          />
        </div>
        <div>
          <label for="device_sync_vlan_group_name" class="block font-bold mb-3"
            >Netbox VLAN group</label
          >
          <UInput
            id="device_sync_vlan_group_name"
            v-model="settings.device_sync_vlan_group_name"
            placeholder="e.g. Global VLANs"
            class="w-full"
          />
          <p class="text-muted-color text-sm mt-2">
            Single global Netbox VLAN Group every synced VLAN is created in, along with each
            interface's untagged/tagged VLAN assignment. Leave empty to disable VLAN sync.
          </p>
        </div>
      </div>
    </div>

    <div class="card">
      <div class="flex flex-wrap gap-2 items-center justify-between mb-4">
        <div class="flex items-center gap-2">
          <h4 class="m-0">Credentials</h4>
          <UButton label="New" icon="i-lucide-plus" color="neutral" size="sm" @click="openNew" />
        </div>
        <UInput v-model="globalFilter" icon="i-lucide-search" placeholder="Search..." />
      </div>
      <p class="text-muted-color text-sm mb-4">
        Login credentials internal/device-sync uses to connect directly to devices. Name is either
        a device name (an override for that one device) or the literal <code>default</code>, used
        for any device without its own entry.
      </p>

      <UTable
        v-model:sorting="sorting"
        v-model:global-filter="globalFilter"
        :data="auths"
        :columns="columns"
        :loading="loading"
        :empty="error ?? 'No device sync credentials found.'"
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
          <div class="flex gap-2">
            <UButton
              icon="i-lucide-pencil"
              variant="outline"
              color="neutral"
              size="sm"
              @click="editDeviceSyncAuth(row.original)"
            />
            <UButton
              icon="i-lucide-trash-2"
              variant="outline"
              color="error"
              size="sm"
              @click="confirmDelete(row.original)"
            />
          </div>
        </template>
      </UTable>
    </div>
  </template>

  <UModal v-model:open="authDialog" title="Device Sync Credentials" :ui="{ content: 'sm:max-w-sm' }">
    <template #body>
      <div class="flex flex-col gap-6">
        <div>
          <label for="name" class="block font-bold mb-3">Name</label>
          <UInput
            id="name"
            v-model.trim="auth.name"
            :color="submitted && !auth.name?.trim() ? 'error' : undefined"
            :highlight="submitted && !auth.name?.trim()"
            placeholder="device name, or &quot;default&quot;"
            autofocus
            class="w-full"
          />
          <small v-if="submitted && !auth.name?.trim()" class="text-red-500">Name is required.</small>
        </div>
        <div>
          <label for="username" class="block font-bold mb-3">Username</label>
          <UInput id="username" v-model="auth.username" class="w-full" />
        </div>
        <div>
          <label for="password" class="block font-bold mb-3">Password</label>
          <PasswordInput id="password" v-model="auth.password" class="w-full" />
          <small v-if="auth.id" class="text-muted-color">Leave blank to keep the current password.</small>
        </div>
      </div>
    </template>

    <template #footer>
      <UButton label="Cancel" icon="i-lucide-x" variant="ghost" @click="hideDialog" />
      <UButton label="Save" icon="i-lucide-check" :loading="saving" @click="saveDeviceSyncAuth" />
    </template>
  </UModal>

  <UModal v-model:open="deleteDialog" title="Confirm" :ui="{ content: 'sm:max-w-sm' }">
    <template #body>
      <span v-if="authToDelete"
        >Delete credentials for <b>{{ authToDelete.name }}</b
        >?</span
      >
    </template>
    <template #footer>
      <UButton label="No" icon="i-lucide-x" variant="ghost" @click="deleteDialog = false" />
      <UButton label="Yes" icon="i-lucide-check" color="error" @click="performDelete" />
    </template>
  </UModal>
</template>
