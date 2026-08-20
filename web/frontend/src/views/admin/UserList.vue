<script setup>
import { useToast } from '@nuxt/ui/composables'
import { onMounted, ref } from 'vue'
import { createUser, deleteUser, getUsers, updateUser } from '@/api/users'
import { getRoles } from '@/api/roles'
import PasswordInput from '@/components/PasswordInput.vue'
import SortableColumnHeader from '@/components/SortableColumnHeader.vue'

const toast = useToast()

const users = ref([])
const roles = ref([])
const loading = ref(true)
const error = ref(null)
const forbidden = ref(false)

const userDialog = ref(false)
const user = ref({})
const submitted = ref(false)
const saving = ref(false)

const deleteDialog = ref(false)
const userToDelete = ref(null)
const deleting = ref(false)

const globalFilter = ref('')
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

function confirmDelete(row) {
  userToDelete.value = row
  deleteDialog.value = true
}

function performDelete() {
  deleting.value = true
  deleteUser(userToDelete.value.id)
    .then(() => {
      toast.add({
        color: 'success',
        title: 'Successful',
        description: 'User deleted',
        duration: 3000,
      })
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
      deleteDialog.value = false
      userToDelete.value = null
    })
}

function hideDialog() {
  userDialog.value = false
  submitted.value = false
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
      <UInput v-model="globalFilter" icon="i-lucide-search" placeholder="Search..." />
    </div>

    <UTable v-model:sorting="sorting" v-model:global-filter="globalFilter" :data="users" :columns="columns"
      :loading="loading" :empty="error ?? 'No users found.'" :virtualize="{ estimateSize: 46 }"
      class="max-h-[calc(100vh-380px)]">
      <template v-for="col in columns.filter((c) => c.accessorKey)" :key="col.accessorKey"
        #[`${col.accessorKey}-header`]="{ column }">
        <SortableColumnHeader :column="column" :label="col.header" />
      </template>

      <template #actions-cell="{ row }">
        <div class="flex gap-2">
          <UButton icon="i-lucide-pencil" variant="outline" color="neutral" size="sm"
            @click="editUser(row.original)" />
          <UButton icon="i-lucide-trash-2" variant="outline" color="error" size="sm"
            :disabled="isAdmin(row.original)" :title="isAdmin(row.original) ? 'The admin user cannot be deleted' : undefined"
            @click="confirmDelete(row.original)" />
        </div>
      </template>
      <template #roles-cell="{ row }">
        <div class="flex flex-wrap gap-1">
          <UBadge v-for="name in roleNames(row.original)" :key="name" :label="name" variant="subtle" />
        </div>
      </template>
    </UTable>
  </div>

  <UModal v-model:open="userDialog" title="User Details" :ui="{ content: 'sm:max-w-sm' }">
    <template #body>
      <div class="flex flex-col gap-6">
        <div>
          <label for="username" class="block font-bold mb-3">Username</label>
          <UInput id="username" v-model.trim="user.username"
            :color="submitted && !user.username?.trim() ? 'error' : undefined"
            :highlight="submitted && !user.username?.trim()" autofocus class="w-full" />
          <small v-if="submitted && !user.username?.trim()" class="text-red-500">Username is required.</small>
        </div>
        <div>
          <label for="name" class="block font-bold mb-3">Name</label>
          <UInput id="name" v-model="user.name" class="w-full" />
        </div>
        <div>
          <label for="email" class="block font-bold mb-3">Email</label>
          <UInput id="email" v-model="user.email" class="w-full" />
        </div>
        <div>
          <label for="mobile" class="block font-bold mb-3">Mobile</label>
          <UInput id="mobile" v-model="user.mobile" class="w-full" />
        </div>
        <div>
          <label for="roles" class="block font-bold mb-3">Roles</label>
          <USelectMenu id="roles" v-model="user.role_ids" :items="roles" value-key="id" label-key="name"
            placeholder="Select Roles" multiple class="w-full" />
        </div>
        <div>
          <label for="password" class="block font-bold mb-3">Password</label>
          <PasswordInput id="password" v-model="user.password"
            :color="submitted && !user.id && !user.password ? 'error' : undefined"
            :highlight="submitted && !user.id && !user.password" />
          <small v-if="submitted && !user.id && !user.password" class="text-red-500">Password is required.</small>
          <small v-else-if="user.id" class="text-muted-color">Leave blank to keep the current password.</small>
        </div>
      </div>
    </template>

    <template #footer>
      <UButton label="Cancel" icon="i-lucide-x" variant="ghost" @click="hideDialog" />
      <UButton label="Save" icon="i-lucide-check" :loading="saving" @click="saveUser" />
    </template>
  </UModal>

  <UModal v-model:open="deleteDialog" title="Confirm" :ui="{ content: 'sm:max-w-sm' }">
    <template #body>
      <span v-if="userToDelete">Delete user <b>{{ userToDelete.username }}</b>?</span>
    </template>
    <template #footer>
      <UButton label="No" icon="i-lucide-x" variant="ghost" @click="deleteDialog = false" />
      <UButton label="Yes" icon="i-lucide-check" color="error" :loading="deleting" @click="performDelete" />
    </template>
  </UModal>
</template>
