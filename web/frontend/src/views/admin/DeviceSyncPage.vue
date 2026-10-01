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
import SearchInput from '@/components/SearchInput.vue'
import SortableColumnHeader from '@/components/SortableColumnHeader.vue'
import { useConfirm } from '@/composables/useConfirm'
import { useSettings } from '@/composables/useSettings'
import { useSearch, valuesText } from '@/utils/search'

const toast = useToast()
const { confirmDelete } = useConfirm()

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

const deleting = ref(false)

const sorting = ref([{ id: 'name', desc: false }])
const { search, filtered } = useSearch(auths, (row) => valuesText(row.id, row.name, row.username))

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

async function performDelete() {
  if (!auth.value?.id) return
  if (!(await confirmDelete(`credentials ${auth.value.name}`))) return
  deleting.value = true
  deleteDeviceSyncAuth(auth.value.id)
    .then(() => {
      toast.add({
        color: 'success',
        title: 'Successful',
        description: 'Credentials deleted',
        duration: 3000,
      })
      authDialog.value = false
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
      deleting.value = false
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
        <UFormField
          label="Enabled"
          hint="Syncs device interfaces/addresses/connections into Netbox. Netbox connection settings are shared with the Netbox tab under Sources settings; per-device login credentials are managed below."
        >
          <USwitch v-model="settings.device_sync_enabled" />
        </UFormField>
        <UFormField label="VRFs allocated in the global table">
          <UTextarea
            v-model="settings.device_sync_vrf_in_global"
            :rows="4"
            placeholder="One VRF name per line"
            class="w-full"
          />
        </UFormField>
        <UFormField label="Netbox device states to sync">
          <UTextarea
            v-model="settings.device_sync_device_states"
            :rows="4"
            placeholder="One state per line, e.g. Active"
            class="w-full"
          />
        </UFormField>
        <UFormField label="Ignore devices">
          <UTextarea
            v-model="settings.device_sync_device_ignore"
            :rows="4"
            placeholder="One device name per line"
            class="w-full"
          />
        </UFormField>
        <UFormField
          label="Netbox VLAN group"
          hint="Single global Netbox VLAN Group every synced VLAN is created in, along with each interface's untagged/tagged VLAN assignment. Leave empty to disable VLAN sync."
        >
          <UInput
            v-model="settings.device_sync_vlan_group_name"
            placeholder="e.g. Global VLANs"
            class="w-full"
          />
        </UFormField>
      </div>
    </div>

    <div class="card">
      <div class="flex flex-wrap gap-2 items-center justify-between mb-4">
        <div class="flex items-center gap-2">
          <h4 class="m-0">Credentials</h4>
          <UButton label="New" icon="i-lucide-plus" color="neutral" size="sm" @click="openNew" />
        </div>
        <SearchInput v-model="search" />
      </div>
      <p class="text-muted-color text-sm mb-4">
        Login credentials used to connect directly to devices: device-sync, service
        push/delete/unrealize, Config-tree rebind, and GUI interface refresh/VLAN push. Name is
        either a device name (an override for that one device) or the literal <code>default</code>,
        used for any device without its own entry.
      </p>

      <UTable
        v-model:sorting="sorting"
        :data="filtered"
        :columns="columns"
        :loading="loading"
        :empty="
          error ||
          (search && auths.length ? 'Nothing matches the search.' : 'No device sync credentials found.')
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
            @click="editDeviceSyncAuth(row.original)"
          />
        </template>
      </UTable>
    </div>
  </template>

  <FormModal v-model:open="authDialog" :source="auth" title="Device Sync Credentials">
    <template #body>
      <div class="flex flex-col gap-4">
        <UFormField label="Name">
          <UInput
            v-model.trim="auth.name"
            :color="submitted && !auth.name?.trim() ? 'error' : undefined"
            :highlight="submitted && !auth.name?.trim()"
            placeholder='device name, or "default"'
            autofocus
            class="w-full"
          />
          <small v-if="submitted && !auth.name?.trim()" class="text-red-500">Name is required.</small>
        </UFormField>
        <UFormField label="Username">
          <UInput v-model="auth.username" class="w-full" />
        </UFormField>
        <UFormField label="Password">
          <PasswordInput v-model="auth.password" class="w-full" />
          <small v-if="auth.id" class="text-muted-color">Leave blank to keep the current password.</small>
        </UFormField>
      </div>
    </template>

    <template #footer="{ close }">
      <div class="flex w-full gap-2">
        <UButton
          v-if="auth.id"
          label="Delete"
          icon="i-lucide-trash"
          color="error"
          variant="ghost"
          :loading="deleting"
          @click="performDelete"
        />
        <UButton class="ms-auto" label="Cancel" icon="i-lucide-x" variant="ghost" @click="close" />
        <UButton label="Save" icon="i-lucide-check" :loading="saving" @click="saveDeviceSyncAuth" />
      </div>
    </template>
  </FormModal>
</template>
