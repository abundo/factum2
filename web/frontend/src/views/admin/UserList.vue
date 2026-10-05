<script setup>
import { useToast } from '@nuxt/ui/composables'
import { onMounted, ref } from 'vue'
import { createUser, deleteUser, getUsers, updateUser } from '@/api/users'
import { getRoles } from '@/api/roles'
import PasswordInput from '@/components/PasswordInput.vue'
import SearchInput from '@/components/SearchInput.vue'
import SortableColumnHeader from '@/components/SortableColumnHeader.vue'
import { useConfirm } from '@/composables/useConfirm'
import { useSearch, valuesText } from '@/utils/search'

const toast = useToast()
const { confirmDelete } = useConfirm()

const users = ref([])
const roles = ref([])
const loading = ref(true)
const error = ref(null)
const forbidden = ref(false)

const userDialog = ref(false)
const user = ref({})
const submitted = ref(false)
const saving = ref(false)

const deleting = ref(false)

const sorting = ref([{ id: 'username', desc: false }])

const columns = [
  { id: 'actions', header: '' },
  { accessorKey: 'name', header: 'Name' },
  { accessorKey: 'username', header: 'Username' },
  { accessorKey: 'email', header: 'Email' },
  { accessorKey: 'mobile', header: 'Mobile' },
  { id: 'roles', header: 'Roles' },
]

function loadUsers() {
  loading.value = true
  error.value = null
  forbidden.value = false
  getUsers()
    .then((data) => {
      users.value = data ?? []
    })
    .catch((err) => {
      if (err.response?.status === 403 || err.response?.status === 401) {
        forbidden.value = true
      } else {
        error.value = 'Failed to load users.'
      }
    })
    .finally(() => {
      loading.value = false
    })
}

function openNew() {
  user.value = { role_ids: [] }
  submitted.value = false
  userDialog.value = true
}

function editUser(row) {
  user.value = { ...row, password: '', role_ids: row.role_ids ?? [] }
  submitted.value = false
  userDialog.value = true
}

function roleNames(row) {
  return (row.role_ids ?? [])
    .map((id) => roles.value.find((r) => r.id === id)?.name)
    .filter(Boolean)
}

function isAdmin(row) {
  return roleNames(row).includes('admin')
}

const { search, filtered } = useSearch(users, (row) =>
  valuesText(row.name, row.username, row.email, row.mobile, roleNames(row)),
)

async function performDelete() {
  if (!user.value?.id || isAdmin(user.value)) return
  if (!(await confirmDelete(`user ${user.value.username}`))) return
  deleting.value = true
  deleteUser(user.value.id)
    .then(() => {
      toast.add({
        color: 'success',
        title: 'Successful',
        description: 'User deleted',
        duration: 3000,
      })
      userDialog.value = false
      loadUsers()
    })
    .catch((err) => {
      toast.add({
        color: 'error',
        title: 'Error',
        description: err.response?.data?.error ?? 'Failed to delete user.',
        duration: 3000,
      })
    })
    .finally(() => {
      deleting.value = false
    })
}

function saveUser() {
  submitted.value = true

  if (!user.value.username?.trim() || (!user.value.id && !user.value.password)) {
    return
  }

  saving.value = true
  const payload = { ...user.value }
  if (!payload.password) {
    delete payload.password
  }
  const request = payload.id ? updateUser(payload.id, payload) : createUser(payload)

  request
    .then(() => {
      toast.add({
        color: 'success',
        title: 'Successful',
        description: payload.id ? 'User updated' : 'User created',
        duration: 3000,
      })
      userDialog.value = false
      loadUsers()
    })
    .catch((err) => {
      toast.add({
        color: 'error',
        title: 'Error',
        description: err.response?.data?.error ?? 'Failed to save user.',
        duration: 3000,
      })
    })
    .finally(() => {
      saving.value = false
    })
}

function loadRoles() {
  getRoles().then((data) => {
    roles.value = data ?? []
  })
}

onMounted(() => {
  loadUsers()
  loadRoles()
})
</script>

<template>
  <div v-if="forbidden" class="card">
    <UAlert color="error" variant="subtle" title="You need administrator permissions to view users." />
  </div>
  <div v-else class="card">
    <div class="flex flex-wrap gap-2 items-center justify-between mb-4">
      <div class="flex items-center gap-2">
        <h4 class="m-0">Users</h4>
        <UButton label="New" icon="i-lucide-plus" color="neutral" size="sm" @click="openNew" />
      </div>
      <SearchInput v-model="search" />
    </div>

    <UTable
      v-model:sorting="sorting"
      :data="filtered"
      :columns="columns"
      :loading="loading"
      :empty="error || (search && users.length ? 'Nothing matches the search.' : 'No users found.')"
      :virtualize="{ estimateSize: 46 }"
      class="max-h-[calc(100vh-380px)]"
    >
      <template v-for="col in columns.filter((c) => c.accessorKey)" :key="col.accessorKey"
        #[`${col.accessorKey}-header`]="{ column }">
        <SortableColumnHeader :column="column" :label="col.header" />
      </template>

      <template #actions-cell="{ row }">
        <UButton
          icon="i-lucide-pencil"
          variant="outline"
          size="sm"
          @click="editUser(row.original)"
        />
      </template>
      <template #roles-cell="{ row }">
        <div class="flex flex-wrap gap-1">
          <UBadge v-for="name in roleNames(row.original)" :key="name" :label="name" variant="subtle" />
        </div>
      </template>
    </UTable>
  </div>

  <FormModal v-model:open="userDialog" :source="user" title="User Details">
    <template #body>
      <div class="flex flex-col gap-4">
        <UFormField label="Username">
          <UInput
            v-model.trim="user.username"
            :color="submitted && !user.username?.trim() ? 'error' : undefined"
            :highlight="submitted && !user.username?.trim()"
            autofocus
            class="w-full"
          />
          <small v-if="submitted && !user.username?.trim()" class="text-red-500">Username is required.</small>
        </UFormField>
        <UFormField label="Name">
          <UInput v-model="user.name" class="w-full" />
        </UFormField>
        <UFormField label="Email">
          <UInput v-model="user.email" class="w-full" />
        </UFormField>
        <UFormField label="Mobile">
          <UInput v-model="user.mobile" class="w-full" />
        </UFormField>
        <UFormField label="Roles">
          <USelectMenu
            v-model="user.role_ids"
            :items="roles"
            value-key="id"
            label-key="name"
            placeholder="Select Roles"
            multiple
            class="w-full"
          />
        </UFormField>
        <UFormField label="Password">
          <PasswordInput
            v-model="user.password"
            :color="submitted && !user.id && !user.password ? 'error' : undefined"
            :highlight="submitted && !user.id && !user.password"
          />
          <small v-if="submitted && !user.id && !user.password" class="text-red-500">Password is required.</small>
          <small v-else-if="user.id" class="text-muted-color">Leave blank to keep the current password.</small>
        </UFormField>
      </div>
    </template>

    <template #footer="{ close }">
      <div class="flex w-full gap-2">
        <UButton
          v-if="user.id && !isAdmin(user)"
          label="Delete"
          icon="i-lucide-trash"
          color="error"
          variant="ghost"
          :loading="deleting"
          @click="performDelete"
        />
        <UButton class="ms-auto" label="Cancel" icon="i-lucide-x" variant="ghost" @click="close" />
        <UButton label="Save" icon="i-lucide-check" :loading="saving" @click="saveUser" />
      </div>
    </template>
  </FormModal>
</template>
