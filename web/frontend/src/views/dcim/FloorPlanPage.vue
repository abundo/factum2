<script setup>
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { onBeforeRouteLeave, useRoute, useRouter } from 'vue-router'
import { useToast } from '@nuxt/ui/composables'
import { getFloorPlan, saveFloorPlanLayout } from '@/api/floorPlans'
import FloorPlanCanvas from '@/components/dcim/FloorPlanCanvas.vue'
import RackFootprint from '@/components/dcim/RackFootprint.vue'
import { useFloorPlanEditor } from '@/composables/useFloorPlanEditor'
import { useAuthStore } from '@/stores/auth'

defineOptions({ name: 'FloorPlanPage' })

const route = useRoute()
const router = useRouter()
const toast = useToast()
const authStore = useAuthStore()
const canWrite = computed(() => authStore.canWrite)

const loading = ref(true)
const error = ref(null)
const saving = ref(false)
const stale = ref(null)
const pickerId = ref(undefined)
const canvasRef = ref(null)
const drawer = ref(false)

const editor = useFloorPlanEditor()
const plan = computed(() => editor.draft.value)

function errMsg(err, fallback) {
  return err.response?.data?.error ?? fallback
}

function load() {
  loading.value = true
  getFloorPlan(route.params.id)
    .then((data) => {
      editor.load(data)
      error.value = null
      stale.value = null
    })
    .catch(() => {
      error.value = 'Failed to load floor plan.'
    })
    .finally(() => {
      loading.value = false
    })
}

function selectedRow() {
  return (plan.value?.racks || []).find((r) => r.rack_id === editor.selectedRackId.value)
}

const availableItems = computed(() =>
  (plan.value?.available_racks || []).map((r) => ({ label: r.name, value: r.id })),
)

function addPicked() {
  const summary = (plan.value?.available_racks || []).find((r) => r.id === pickerId.value)
  if (!summary) return
  if (!editor.addRack(summary, plan.value.grid_mm, plan.value.grid_mm)) {
    toast.add({ color: 'error', title: 'Could not place rack on the plan' })
  }
  pickerId.value = undefined
}

function save() {
  if (!plan.value) return
  saving.value = true
  saveFloorPlanLayout(plan.value.id, editor.layoutPayload(plan.value))
    .then((data) => {
      editor.acceptSaved(data)
      stale.value = null
      toast.add({ color: 'success', title: 'Floor plan saved', duration: 2500 })
    })
    .catch((err) => {
      if (err.response?.status === 409) {
        stale.value = errMsg(err, 'The plan was saved by someone else.')
        return
      }
      toast.add({ color: 'error', title: 'Save failed', description: errMsg(err) })
    })
    .finally(() => {
      saving.value = false
    })
}

function reloadKeepDraft() {
  getFloorPlan(route.params.id).then((data) => {
    const draft = editor.layoutPayload(editor.draft.value)
    editor.acceptSaved(data)
    editor.editMode.value = true
    draft.revision = data.revision
    saveFloorPlanLayout(data.id, draft)
      .then((saved) => {
        editor.acceptSaved(saved)
        stale.value = null
      })
      .catch((err) =>
        toast.add({ color: 'error', title: 'Reapply failed', description: errMsg(err) }),
      )
  })
}

function onKey(ev) {
  if (!editor.editMode.value) return
  if ((ev.metaKey || ev.ctrlKey) && ev.key === 'z') {
    ev.preventDefault()
    if (ev.shiftKey) editor.redo()
    else editor.undo()
  }
}

onMounted(() => {
  load()
  window.addEventListener('keydown', onKey)
})
onBeforeUnmount(() => window.removeEventListener('keydown', onKey))

onBeforeRouteLeave(() => {
  if (editor.dirty.value) {
    return window.confirm('Discard unsaved floor-plan changes?')
  }
  return true
})
</script>

<template>
  <div class="flex min-h-0 flex-1 flex-col gap-3 overflow-hidden">
    <div class="flex flex-wrap items-center justify-between gap-2 shrink-0">
      <div class="flex items-center gap-2">
        <UButton
          icon="i-lucide-arrow-left"
          variant="ghost"
          color="neutral"
          size="sm"
          @click="router.push('/dcim/floor-plans')"
        />
        <h4 class="m-0">{{ plan?.name || 'Floor plan' }}</h4>
        <UBadge v-if="editor.dirty.value" label="Unsaved" color="warning" variant="subtle" />
      </div>
      <div class="flex flex-wrap gap-2">
        <UButton
          label="Fit"
          size="sm"
          variant="outline"
          color="neutral"
          @click="canvasRef?.fit()"
        />
        <UButton
          v-if="canWrite"
          :label="editor.editMode.value ? 'Editing' : 'Edit'"
          size="sm"
          :variant="editor.editMode.value ? 'solid' : 'outline'"
          @click="editor.editMode.value = !editor.editMode.value"
        />
        <UButton
          v-if="editor.editMode.value"
          label="Undo"
          size="sm"
          variant="ghost"
          :disabled="!editor.history.value.length"
          @click="editor.undo()"
        />
        <UButton
          v-if="editor.editMode.value"
          label="Cancel"
          size="sm"
          variant="ghost"
          @click="editor.cancel()"
        />
        <UButton
          v-if="canWrite"
          label="Save"
          size="sm"
          :disabled="!editor.dirty.value"
          :loading="saving"
          @click="save"
        />
      </div>
    </div>

    <UAlert
      v-if="stale"
      color="warning"
      title="Save conflict"
      :description="stale + ' Reload the saved plan or reapply your draft on top of it.'"
    >
      <template #actions>
        <UButton size="sm" variant="outline" label="Reload" @click="load" />
        <UButton size="sm" label="Reapply draft" @click="reloadKeepDraft" />
      </template>
    </UAlert>

    <div v-if="error" class="text-error">{{ error }}</div>
    <div v-else-if="loading && !plan" class="text-muted">Loading…</div>
    <div v-else-if="plan" class="flex min-h-0 flex-1 gap-4 overflow-hidden">
      <div class="min-w-0 flex-1">
        <FloorPlanCanvas
          ref="canvasRef"
          :plan="plan"
          :edit-mode="editor.editMode.value"
          :selected-rack-id="editor.selectedRackId.value"
          @select="
            (id) => {
              editor.selectedRackId.value = id
              drawer = true
            }
          "
          @move="
            ({ rackId, x, y }) => {
              if (!editor.moveRack(rackId, x, y))
                toast.add({ color: 'error', title: 'Invalid position' })
            }
          "
          @rotate="
            (id) => {
              if (!editor.rotateRack(id))
                toast.add({ color: 'error', title: 'Rotation does not fit' })
            }
          "
        />
      </div>
      <div class="w-80 shrink-0 flex flex-col gap-3 overflow-auto">
        <div v-if="editor.editMode.value && canWrite">
          <UFormField label="Add rack">
            <div class="flex gap-2">
              <USelect v-model="pickerId" :items="availableItems" class="flex-1" />
              <UButton label="Add" size="sm" @click="addPicked" />
            </div>
          </UFormField>
        </div>
        <div>
          <div class="text-sm font-medium mb-1">Racks on this plan</div>
          <div v-for="row in plan.racks" :key="row.rack_id" class="py-1">
            <button
              class="text-left w-full"
              type="button"
              @click="editor.selectedRackId.value = row.rack_id"
            >
              <RackFootprint :row="row" />
            </button>
          </div>
        </div>
        <div class="text-xs text-muted">
          Occupancy: grey empty, green low, blue mid, orange high, yellow missing height.
        </div>
      </div>
    </div>

    <USlideover v-model:open="drawer" title="Rack">
      <div v-if="selectedRow()" class="flex flex-col gap-3 p-4">
        <h5 class="m-0">{{ selectedRow().rack?.name }}</h5>
        <div class="text-sm">{{ selectedRow().rack?.occupancy_percent || 0 }}% occupied</div>
        <UFormField v-if="editor.editMode.value" label="X (mm)">
          <UInput
            :model-value="selectedRow().x_mm"
            @update:model-value="(v) => editor.moveRack(selectedRow().rack_id, Number(v), selectedRow().y_mm)"
          />
        </UFormField>
        <UFormField v-if="editor.editMode.value" label="Y (mm)">
          <UInput
            :model-value="selectedRow().y_mm"
            @update:model-value="(v) => editor.moveRack(selectedRow().rack_id, selectedRow().x_mm, Number(v))"
          />
        </UFormField>
        <div class="flex gap-2">
          <UButton
            label="Elevation"
            size="sm"
            @click="router.push(`/dcim/racks/${selectedRow().rack_id}`)"
          />
          <UButton
            v-if="editor.editMode.value"
            label="Rotate 90°"
            size="sm"
            variant="outline"
            @click="editor.rotateRack(selectedRow().rack_id)"
          />
          <UButton
            v-if="editor.editMode.value"
            label="Remove from plan"
            size="sm"
            color="error"
            variant="ghost"
            @click="editor.removeRack(selectedRow().rack_id)"
          />
        </div>
      </div>
    </USlideover>
  </div>
</template>
