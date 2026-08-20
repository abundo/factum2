<script setup>
import { useToast } from '@nuxt/ui/composables'
import { computed, onMounted, ref } from 'vue'
import draggable from 'vuedraggable'
import { createLink, deleteLink, getAdminLinks, reorderLinks, updateLink } from '@/api/links'

const toast = useToast()

const MAX_ICON_BYTES = 300 * 1024

const links = ref([])
const loading = ref(true)
const error = ref(null)
const forbidden = ref(false)

const linkDialog = ref(false)
const link = ref({})
const iconFile = ref(null)
const submitted = ref(false)
const saving = ref(false)
const deleteDialog = ref(false)
const linkToDelete = ref(null)

const groupOptions = computed(() => [...new Set(links.value.map((l) => l.group).filter(Boolean))])

function loadLinks() {
  loading.value = true
  error.value = null
  forbidden.value = false
  getAdminLinks()
    .then((data) => {
      links.value = data ?? []
    })
    .catch((err) => {
      if (err.response?.status === 403 || err.response?.status === 401) {
        forbidden.value = true
      } else {
        error.value = 'Failed to load links.'
      }
    })
    .finally(() => {
      loading.value = false
    })
}

function openNew() {
  link.value = { open_in_new_tab: true }
  iconFile.value = null
  submitted.value = false
  linkDialog.value = true
}

function editLink(row) {
  link.value = { ...row }
  iconFile.value = null
  submitted.value = false
  linkDialog.value = true
}

function hideDialog() {
  linkDialog.value = false
  submitted.value = false
}

function onIconFileChange(file) {
  if (!file) return
  if (file.size > MAX_ICON_BYTES) {
    toast.add({
      color: 'error',
      title: 'Error',
      description: 'Icon must be smaller than 300 KB.',
      duration: 3000,
    })
    iconFile.value = null
    return
  }
  const reader = new FileReader()
  reader.onload = () => {
    link.value.icon = reader.result
  }
  reader.readAsDataURL(file)
}

function removeIcon() {
  link.value.icon = ''
  iconFile.value = null
}

function saveLink() {
  submitted.value = true

  if (!link.value.group?.trim() || !link.value.name?.trim() || !link.value.url?.trim()) {
    return
  }

  saving.value = true
  const payload = { ...link.value }
  const request = payload.id ? updateLink(payload.id, payload) : createLink(payload)

  request
    .then(() => {
      toast.add({
        color: 'success',
        title: 'Successful',
        description: payload.id ? 'Link updated' : 'Link created',
        duration: 3000,
      })
      linkDialog.value = false
      loadLinks()
    })
    .catch((err) => {
      toast.add({
        color: 'error',
        title: 'Error',
        description: err.response?.data?.error ?? 'Failed to save link.',
        duration: 3000,
      })
    })
    .finally(() => {
      saving.value = false
    })
}

function confirmDelete(row) {
  linkToDelete.value = row
  deleteDialog.value = true
}

function performDelete() {
  deleteLink(linkToDelete.value.id)
    .then(() => {
      toast.add({
        color: 'success',
        title: 'Successful',
        description: 'Link deleted',
        duration: 3000,
      })
      loadLinks()
    })
    .catch((err) => {
      toast.add({
        color: 'error',
        title: 'Error',
        description: err.response?.data?.error ?? 'Failed to delete link.',
        duration: 3000,
      })
    })
    .finally(() => {
      deleteDialog.value = false
      linkToDelete.value = null
    })
}

function onReorder() {
  reorderLinks(links.value.map((l) => l.id)).catch((err) => {
    toast.add({
      color: 'error',
      title: 'Error',
      description: err.response?.data?.error ?? 'Failed to save new order.',
      duration: 3000,
    })
    loadLinks()
  })
}

onMounted(loadLinks)
</script>

<template>
  <div v-if="forbidden" class="card">
    <UAlert
      color="error"
      variant="subtle"
      title="You need administrator permissions to view dashboard settings."
    />
  </div>
  <template v-else>
    <div class="card">
      <div class="flex items-center justify-between mb-2">
        <h4 class="m-0 font-semibold text-lg">Dashboard links</h4>
        <UButton label="New" icon="i-lucide-plus" color="neutral" @click="openNew" />
      </div>
      <p class="text-muted-color mb-4">
        Shortcuts shown on the dashboard, grouped by "Group". Drag the handle to reorder.
      </p>

      <div v-if="loading" class="text-muted-color py-4">Loading...</div>
      <div v-else-if="error" class="text-muted-color py-4">{{ error }}</div>
      <div v-else-if="!links.length" class="text-muted-color py-4">No links found.</div>
      <draggable
        v-else
        v-model="links"
        item-key="id"
        handle=".drag-handle"
        tag="div"
        class="table w-full border-collapse divide-y divide-default"
        @end="onReorder"
      >
        <template #item="{ element }">
          <div class="table-row">
            <div class="table-cell align-middle py-3 pr-3">
              <UIcon
                name="i-lucide-grip-vertical"
                class="drag-handle cursor-grab text-muted-color"
              />
            </div>
            <div class="table-cell align-middle py-5 pr-10">
              <img
                v-if="element.icon"
                :src="element.icon"
                class="size-15 rounded object-contain"
              />
              <UIcon v-else name="i-lucide-link" class="size-15 text-muted-color" />
            </div>
            <div class="table-cell align-middle py-3 pr-3">
              <UBadge :label="element.group" variant="subtle" />
            </div>
            <div class="table-cell align-middle py-3 pr-3 w-full min-w-0">
              <div class="font-medium truncate">{{ element.name }}</div>
              <div class="text-sm text-muted-color truncate">{{ element.url }}</div>
            </div>
            <div class="table-cell align-middle py-3 pr-3">
              <UIcon
                v-if="element.open_in_new_tab"
                name="i-lucide-external-link"
                class="text-muted-color"
              />
            </div>
            <div class="table-cell align-middle py-3">
              <div class="flex gap-2">
                <UButton
                  icon="i-lucide-pencil"
                  variant="outline"
                  color="neutral"
                  size="sm"
                  @click="editLink(element)"
                />
                <UButton
                  icon="i-lucide-trash-2"
                  variant="outline"
                  color="error"
                  size="sm"
                  @click="confirmDelete(element)"
                />
              </div>
            </div>
          </div>
        </template>
      </draggable>
    </div>
  </template>

  <UModal v-model:open="linkDialog" title="Link Details" :ui="{ content: 'sm:max-w-md' }">
    <template #body>
      <div class="flex flex-col gap-6">
        <div>
          <label for="group" class="block font-bold mb-3">Group</label>
          <UInput
            id="group"
            v-model.trim="link.group"
            list="link-group-options"
            :color="submitted && !link.group?.trim() ? 'error' : undefined"
            :highlight="submitted && !link.group?.trim()"
            placeholder="e.g. Monitoring"
            autofocus
            class="w-full"
          />
          <datalist id="link-group-options">
            <option v-for="g in groupOptions" :key="g" :value="g" />
          </datalist>
          <small v-if="submitted && !link.group?.trim()" class="text-red-500"
            >Group is required.</small
          >
        </div>
        <div>
          <label for="name" class="block font-bold mb-3">Name</label>
          <UInput
            id="name"
            v-model.trim="link.name"
            :color="submitted && !link.name?.trim() ? 'error' : undefined"
            :highlight="submitted && !link.name?.trim()"
            class="w-full"
          />
          <small v-if="submitted && !link.name?.trim()" class="text-red-500"
            >Name is required.</small
          >
        </div>
        <div>
          <label for="url" class="block font-bold mb-3">URL</label>
          <UInput
            id="url"
            v-model.trim="link.url"
            placeholder="https://example.com"
            :color="submitted && !link.url?.trim() ? 'error' : undefined"
            :highlight="submitted && !link.url?.trim()"
            class="w-full"
          />
          <small v-if="submitted && !link.url?.trim()" class="text-red-500"
            >URL is required.</small
          >
        </div>
        <div>
          <UCheckbox v-model="link.open_in_new_tab" label="Open in new tab" />
        </div>
        <div>
          <label class="block font-bold mb-3">Icon (optional)</label>
          <div class="flex items-center gap-3">
            <img
              v-if="link.icon"
              :src="link.icon"
              class="size-10 rounded object-contain border border-default"
            />
            <UFileUpload
              v-model="iconFile"
              accept="image/*"
              icon="i-lucide-image"
              label="Upload icon"
              variant="button"
              @update:model-value="onIconFileChange"
            />
            <UButton
              v-if="link.icon"
              label="Remove"
              icon="i-lucide-x"
              variant="ghost"
              color="neutral"
              size="sm"
              @click="removeIcon"
            />
          </div>
        </div>
      </div>
    </template>

    <template #footer>
      <UButton label="Cancel" icon="i-lucide-x" variant="ghost" @click="hideDialog" />
      <UButton label="Save" icon="i-lucide-check" :loading="saving" @click="saveLink" />
    </template>
  </UModal>

  <UModal v-model:open="deleteDialog" title="Confirm" :ui="{ content: 'sm:max-w-sm' }">
    <template #body>
      <div class="flex items-center gap-4">
        <UIcon name="i-lucide-triangle-alert" class="size-8 text-warning" />
        <span v-if="linkToDelete"
          >Delete link <b>{{ linkToDelete.name }}</b
          >?</span
        >
      </div>
    </template>
    <template #footer>
      <UButton label="No" icon="i-lucide-x" variant="ghost" @click="deleteDialog = false" />
      <UButton label="Yes" icon="i-lucide-check" color="error" @click="performDelete" />
    </template>
  </UModal>
</template>
