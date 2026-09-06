<script setup>
import { useToast } from '@nuxt/ui/composables'
import { onMounted, ref } from 'vue'
import { createRole, getRoles, updateRole } from '@/api/roles'
import SearchInput from '@/components/SearchInput.vue'
import SortableColumnHeader from '@/components/SortableColumnHeader.vue'

const toast = useToast()

const roles = ref([])
const loading = ref(true)
const error = ref(null)
const forbidden = ref(false)

const roleDialog = ref(false)
const role = ref({})
const submitted = ref(false)
const saving = ref(false)

const globalFilter = ref('')
const sorting = ref([{ id: 'name', desc: false }])

const columns = [
  { id: 'actions', header: '' },
  { accessorKey: 'name', header: 'Name' },
  { accessorKey: 'description', header: 'Description' },
]

function loadRoles() {
  loading.value = true
  error.value = null
  forbidden.value = false
  getRoles()
    .then((data) => {
      roles.value = data ?? []
    })
    .catch((err) => {
      if (err.response?.status === 403 || err.response?.status === 401) {
        forbidden.value = true
      } else {
        error.value = 'Failed to load roles.'
      }
    })
    .finally(() => {
      loading.value = false
    })
}

function openNew() {
  role.value = {}
  submitted.value = false
  roleDialog.value = true
}

function editRole(row) {
  role.value = { ...row }
  submitted.value = false
  roleDialog.value = true
}

function hideDialog() {
  roleDialog.value = false
  submitted.value = false
}

function saveRole() {
  submitted.value = true

  if (!role.value.name?.trim()) {
    return
  }

  saving.value = true
  const payload = { ...role.value }
  const request = payload.id ? updateRole(payload.id, payload) : createRole(payload)

  request
    .then(() => {
      toast.add({
        color: 'success',
        title: 'Successful',
        description: payload.id ? 'Role updated' : 'Role created',
        duration: 3000,
      })
      roleDialog.value = false
      loadRoles()
    })
    .catch((err) => {
      toast.add({
        color: 'error',
        title: 'Error',
        description: err.response?.data?.error ?? 'Failed to save role.',
        duration: 3000,
      })
    })
    .finally(() => {
      saving.value = false
    })
}

onMounted(loadRoles)
</script>

<template>
  <div v-if="forbidden" class="card">
    <UAlert color="error" variant="subtle" title="You need administrator permissions to view roles." />
  </div>
  <div v-else class="card">
    <div class="flex flex-wrap gap-2 items-center justify-between mb-4">
      <div class="flex items-center gap-2">
        <h4 class="m-0">Roles</h4>
        <UButton label="New" icon="i-lucide-plus" color="neutral" size="sm" @click="openNew" />
      </div>
      <SearchInput v-model="globalFilter" />
    </div>

    <UTable
      v-model:sorting="sorting"
      v-model:global-filter="globalFilter"
      :data="roles"
      :columns="columns"
      :loading="loading"
      :empty="error ?? 'No roles found.'"
      :virtualize="{ estimateSize: 46 }"
      class="max-h-[calc(100vh-380px)]"
    >
      <template
        v-for="col in columns.filter((c) => c.id !== 'actions')"
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
          @click="editRole(row.original)"
        />
      </template>
    </UTable>
  </div>

  <UModal v-model:open="roleDialog" title="Role Details" :ui="{ content: 'sm:max-w-sm' }">
    <template #body>
      <div class="flex flex-col gap-6">
        <div>
          <label for="name" class="block font-bold mb-3">Name</label>
          <UInput
            id="name"
            v-model.trim="role.name"
            :color="submitted && !role.name?.trim() ? 'error' : undefined"
            :highlight="submitted && !role.name?.trim()"
            autofocus
            class="w-full"
          />
          <small v-if="submitted && !role.name?.trim()" class="text-red-500">Name is required.</small>
        </div>
        <div>
          <label for="description" class="block font-bold mb-3">Description</label>
          <UTextarea id="description" v-model="role.description" :rows="3" class="w-full" />
        </div>
      </div>
    </template>

    <template #footer>
      <UButton label="Cancel" icon="i-lucide-x" variant="ghost" @click="hideDialog" />
      <UButton label="Save" icon="i-lucide-check" :loading="saving" @click="saveRole" />
    </template>
  </UModal>
</template>
