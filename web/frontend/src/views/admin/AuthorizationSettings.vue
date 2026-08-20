<script setup>
import { useToast } from '@nuxt/ui/composables'
import { computed, onMounted, reactive, ref } from 'vue'
import { getRoles } from '@/api/roles'
import { getSettings, updateSettings } from '@/api/settings'
import {
  createLdapRoleMapping,
  deleteLdapRoleMapping,
  getLdapRoleMappings,
  updateLdapRoleMapping,
} from '@/api/ldapRoleMappings'
import LdapTreeBrowser from '@/components/LdapTreeBrowser.vue'
import SortableColumnHeader from '@/components/SortableColumnHeader.vue'

const toast = useToast()

const mappings = ref([])
const roles = ref([])
const settings = reactive({})
const loading = ref(true)
const error = ref(null)
const forbidden = ref(false)

const mappingDialog = ref(false)
const mapping = ref({})
const submitted = ref(false)
const saving = ref(false)
const browserVisible = ref(false)
const savingDefaultRole = ref(false)
const deleteDialog = ref(false)
const mappingToDelete = ref(null)

const globalFilter = ref('')

const columns = [
  { id: 'actions', header: '' },
  { accessorKey: 'group_dn', header: 'Group DN' },
  { id: 'role', header: 'Role' },
]

const roleOptions = computed(() => roles.value.map((r) => ({ label: r.name, value: r.id })))
const defaultRoleOptions = computed(() => [{ label: 'None', value: null }, ...roleOptions.value])

function roleName(roleId) {
  return roles.value.find((r) => r.id === roleId)?.name ?? '(unknown role)'
}

function loadMappings() {
  loading.value = true
  error.value = null
  forbidden.value = false
  getLdapRoleMappings()
    .then((data) => {
      mappings.value = data ?? []
    })
    .catch((err) => {
      if (err.response?.status === 403 || err.response?.status === 401) {
        forbidden.value = true
      } else {
        error.value = 'Failed to load group mappings.'
      }
    })
    .finally(() => {
      loading.value = false
    })
}

function loadRoles() {
  getRoles().then((data) => {
    roles.value = data ?? []
  })
}

function loadSettings() {
  getSettings().then((data) => {
    Object.assign(settings, data)
  })
}

function openNew() {
  mapping.value = {}
  submitted.value = false
  mappingDialog.value = true
}

function editMapping(row) {
  mapping.value = { ...row }
  submitted.value = false
  mappingDialog.value = true
}

function hideDialog() {
  mappingDialog.value = false
  submitted.value = false
}

function onGroupDnSelected(dn) {
  mapping.value.group_dn = dn
}

function saveMapping() {
  submitted.value = true

  if (!mapping.value.group_dn?.trim() || !mapping.value.role_id) {
    return
  }

  saving.value = true
  const payload = { ...mapping.value }
  const request = payload.id
    ? updateLdapRoleMapping(payload.id, payload)
    : createLdapRoleMapping(payload)

  request
    .then(() => {
      toast.add({
        color: 'success',
        title: 'Successful',
        description: payload.id ? 'Mapping updated' : 'Mapping created',
        duration: 3000,
      })
      mappingDialog.value = false
      loadMappings()
    })
    .catch((err) => {
      toast.add({
        color: 'error',
        title: 'Error',
        description: err.response?.data?.error ?? 'Failed to save mapping.',
        duration: 3000,
      })
    })
    .finally(() => {
      saving.value = false
    })
}

function confirmDelete(row) {
  mappingToDelete.value = row
  deleteDialog.value = true
}

function performDelete() {
  deleteLdapRoleMapping(mappingToDelete.value.id)
    .then(() => {
      toast.add({
        color: 'success',
        title: 'Successful',
        description: 'Mapping deleted',
        duration: 3000,
      })
      loadMappings()
    })
    .catch((err) => {
      toast.add({
        color: 'error',
        title: 'Error',
        description: err.response?.data?.error ?? 'Failed to delete mapping.',
        duration: 3000,
      })
    })
    .finally(() => {
      deleteDialog.value = false
      mappingToDelete.value = null
    })
}

function saveDefaultRole() {
  savingDefaultRole.value = true
  updateSettings({ ldap_default_role_id: settings.ldap_default_role_id })
    .then((data) => {
      Object.assign(settings, data)
      toast.add({
        color: 'success',
        title: 'Successful',
        description: 'Default role saved',
        duration: 3000,
      })
    })
    .catch((err) => {
      toast.add({
        color: 'error',
        title: 'Error',
        description: err.response?.data?.error ?? 'Failed to save default role.',
        duration: 3000,
      })
    })
    .finally(() => {
      savingDefaultRole.value = false
    })
}

onMounted(() => {
  loadMappings()
  loadRoles()
  loadSettings()
})
</script>

<template>
  <div v-if="forbidden" class="card">
    <UAlert
      color="error"
      variant="subtle"
      title="You need administrator permissions to view authorization settings."
    />
  </div>
  <template v-else>
    <div class="card mb-6">
      <div class="font-semibold text-lg mb-3">Default role</div>
      <p class="text-muted-color mb-4">
        Role granted to an LDAP/AD-authenticated user whose group memberships don't match any
        mapping below.
      </p>
      <div class="flex items-end gap-4">
        <div class="w-64">
          <USelect
            id="ldap_default_role_id"
            v-model="settings.ldap_default_role_id"
            :items="defaultRoleOptions"
            class="w-full"
          />
        </div>
        <UButton
          label="Save"
          icon="i-lucide-check"
          :loading="savingDefaultRole"
          @click="saveDefaultRole"
        />
      </div>
    </div>

    <div class="card">
      <div class="flex items-center justify-between mb-6">
        <h4 class="m-0 font-semibold text-lg">LDAP/AD group -&gt; role mappings</h4>
        <div class="flex items-center gap-2">
          <UInput
            v-model="globalFilter"
            icon="i-lucide-search"
            placeholder="Search..."
            class="w-64"
          />
          <UButton label="New" icon="i-lucide-plus" color="neutral" @click="openNew" />
        </div>
      </div>

      <UTable
        v-model:global-filter="globalFilter"
        :data="mappings"
        :columns="columns"
        :loading="loading"
        :empty="error ?? 'No mappings found.'"
        class="max-h-[calc(100vh-480px)]"
      >
        <template #group_dn-header="{ column }">
          <SortableColumnHeader :column="column" label="Group DN" />
        </template>
        <template #actions-cell="{ row }">
          <div class="flex gap-2">
            <UButton
              icon="i-lucide-pencil"
              variant="outline"
              color="neutral"
              size="sm"
              @click="editMapping(row.original)"
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
        <template #role-cell="{ row }">
          <UBadge :label="roleName(row.original.role_id)" variant="subtle" />
        </template>
      </UTable>
    </div>
  </template>

  <UModal v-model:open="mappingDialog" title="Group Mapping Details" :ui="{ content: 'sm:max-w-md' }">
    <template #body>
      <div class="flex flex-col gap-6">
        <div>
          <label for="group_dn" class="block font-bold mb-3">Group DN</label>
          <div class="flex gap-2">
            <UInput
              id="group_dn"
              v-model.trim="mapping.group_dn"
              :color="submitted && !mapping.group_dn?.trim() ? 'error' : undefined"
              :highlight="submitted && !mapping.group_dn?.trim()"
              placeholder="CN=admins,OU=groups,DC=example,DC=com"
              autofocus
              class="w-full"
            />
            <UButton
              icon="i-lucide-network"
              label="Browse"
              variant="outline"
              color="neutral"
              @click="browserVisible = true"
            />
          </div>
          <small v-if="submitted && !mapping.group_dn?.trim()" class="text-red-500"
            >Group DN is required.</small
          >
        </div>
        <div>
          <label for="role_id" class="block font-bold mb-3">Role</label>
          <USelect
            id="role_id"
            v-model="mapping.role_id"
            :items="roleOptions"
            :color="submitted && !mapping.role_id ? 'error' : undefined"
            :highlight="submitted && !mapping.role_id"
            class="w-full"
          />
          <small v-if="submitted && !mapping.role_id" class="text-red-500">Role is required.</small>
        </div>
      </div>
    </template>

    <template #footer>
      <UButton label="Cancel" icon="i-lucide-x" variant="ghost" @click="hideDialog" />
      <UButton label="Save" icon="i-lucide-check" :loading="saving" @click="saveMapping" />
    </template>
  </UModal>

  <UModal v-model:open="deleteDialog" title="Confirm" :ui="{ content: 'sm:max-w-sm' }">
    <template #body>
      <div class="flex items-center gap-4">
        <UIcon name="i-lucide-triangle-alert" class="size-8 text-warning" />
        <span v-if="mappingToDelete"
          >Delete mapping for <b>{{ mappingToDelete.group_dn }}</b
          >?</span
        >
      </div>
    </template>
    <template #footer>
      <UButton label="No" icon="i-lucide-x" variant="ghost" @click="deleteDialog = false" />
      <UButton label="Yes" icon="i-lucide-check" color="error" @click="performDelete" />
    </template>
  </UModal>

  <LdapTreeBrowser v-model:visible="browserVisible" @select="onGroupDnSelected" />
</template>
