<script setup>
import { computed, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useToast } from '@nuxt/ui/composables'
import { createFloorPlan, getFloorPlans, renameFloorPlan } from '@/api/floorPlans'
import { getSites } from '@/api/sites'
import FormModal from '@/components/FormModal.vue'
import SearchInput from '@/components/SearchInput.vue'
import { useAuthStore } from '@/stores/auth'

defineOptions({ name: 'FloorPlanListPage' })

const route = useRoute()
const router = useRouter()
const toast = useToast()
const authStore = useAuthStore()
const canWrite = computed(() => authStore.canWrite)

const items = ref([])
const sites = ref([])
const loading = ref(true)
const error = ref(null)
const globalFilter = ref('')
const dialog = ref(false)
const renameDialog = ref(false)
const saving = ref(false)
const renaming = ref(false)
const form = ref(emptyForm())
const renameForm = ref({ id: 0, name: '' })

function emptyForm() {
  return { name: '', site_id: Number(route.query.site_id) || undefined }
}

function errMsg(err, fallback) {
  return err.response?.data?.error ?? fallback
}

const siteItems = computed(() => sites.value.map((s) => ({ label: s.name, value: s.id })))
const filtered = computed(() => {
  const q = globalFilter.value.trim().toLowerCase()
  if (!q) return items.value
  return items.value.filter((p) => `${p.name} ${p.site_id}`.toLowerCase().includes(q))
})

function load() {
  loading.value = true
  const params = {}
  if (route.query.site_id) params.site_id = route.query.site_id
  Promise.all([getFloorPlans(params), getSites()])
    .then(([plans, siteRows]) => {
      items.value = plans ?? []
      sites.value = siteRows ?? []
      error.value = null
    })
    .catch(() => {
      error.value = 'Failed to load floor plans.'
    })
    .finally(() => {
      loading.value = false
    })
}

function openNew() {
  form.value = emptyForm()
  dialog.value = true
}

function openRename(plan) {
  renameForm.value = { id: plan.id, name: plan.name ?? '' }
  renameDialog.value = true
}

function saveRename() {
  const name = renameForm.value.name?.trim()
  if (!name) {
    toast.add({ color: 'error', title: 'Name is required' })
    return
  }
  renaming.value = true
  renameFloorPlan(renameForm.value.id, name)
    .then((row) => {
      renameDialog.value = false
      const i = items.value.findIndex((p) => p.id === row.id)
      if (i >= 0) items.value[i] = { ...items.value[i], ...row }
    })
    .catch((err) => toast.add({ color: 'error', title: 'Rename failed', description: errMsg(err) }))
    .finally(() => {
      renaming.value = false
    })
}

function save() {
  if (!form.value.name?.trim() || !form.value.site_id) {
    toast.add({ color: 'error', title: 'Name and site are required' })
    return
  }
  saving.value = true
  createFloorPlan({ name: form.value.name.trim(), site_id: form.value.site_id })
    .then((row) => {
      dialog.value = false
      router.push(`/dcim/floor-plans/${row.id}`)
    })
    .catch((err) => toast.add({ color: 'error', title: 'Create failed', description: errMsg(err) }))
    .finally(() => {
      saving.value = false
    })
}

onMounted(load)
</script>

<template>
  <div class="card flex min-h-0 flex-1 flex-col overflow-hidden">
    <div class="flex flex-wrap gap-2 items-center justify-between mb-4 shrink-0">
      <div class="flex items-center gap-2">
        <h4 class="m-0">Floor plans</h4>
        <UButton
          v-if="canWrite"
          label="New"
          icon="i-lucide-plus"
          color="neutral"
          size="sm"
          @click="openNew"
        />
      </div>
      <SearchInput v-model="globalFilter" />
    </div>
    <p class="text-muted text-sm mb-3">
      Floor drawings are stored in Factum. Moving a rack on a plan does not change its site.
    </p>
    <div v-if="error" class="text-error">{{ error }}</div>
    <div v-else-if="loading" class="text-muted">Loading…</div>
    <ul v-else class="flex flex-col gap-2 overflow-auto">
      <li v-for="p in filtered" :key="p.id" class="flex items-stretch gap-1">
        <button
          class="min-w-0 flex-1 text-left px-3 py-2 rounded border border-default hover:bg-elevated"
          type="button"
          @click="router.push(`/dcim/floor-plans/${p.id}`)"
        >
          <div class="font-medium">{{ p.name }}</div>
          <div class="text-sm text-muted">
            {{ p.width_mm }} × {{ p.height_mm }} mm · revision {{ p.revision }}
          </div>
        </button>
        <UButton
          v-if="canWrite"
          icon="i-lucide-pencil"
          variant="outline"
          color="neutral"
          size="sm"
          class="shrink-0 self-center"
          aria-label="Rename floor plan"
          @click="openRename(p)"
        />
      </li>
      <li v-if="!filtered.length" class="text-muted text-sm">No floor plans.</li>
    </ul>
  </div>

  <FormModal v-model:open="dialog" :source="form" title="New floor plan">
    <template #body>
      <div class="flex flex-col gap-4">
        <UFormField label="Name">
          <UInput v-model="form.name" class="w-full" autofocus />
        </UFormField>
        <UFormField label="Site">
          <USelect v-model="form.site_id" :items="siteItems" class="w-full" />
        </UFormField>
      </div>
    </template>
    <template #footer>
      <UButton label="Cancel" variant="ghost" @click="dialog = false" />
      <UButton label="Create" :loading="saving" @click="save" />
    </template>
  </FormModal>

  <FormModal v-model:open="renameDialog" :source="renameForm" title="Rename floor plan">
    <template #body>
      <UFormField label="Name">
        <UInput
          v-model="renameForm.name"
          class="w-full"
          autofocus
          @keydown.enter.prevent="saveRename"
        />
      </UFormField>
    </template>
    <template #footer>
      <UButton label="Cancel" variant="ghost" @click="renameDialog = false" />
      <UButton label="Rename" :loading="renaming" @click="saveRename" />
    </template>
  </FormModal>
</template>
