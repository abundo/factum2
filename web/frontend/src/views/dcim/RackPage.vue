<script setup>
import { computed, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useToast } from '@nuxt/ui/composables'
import { getRackElevation, placeDevice, unmountDevice } from '@/api/racks'
import RackElevation from '@/components/dcim/RackElevation.vue'
import { useAuthStore } from '@/stores/auth'
import { snapOffsetToWholeU, TICKS_PER_U } from '@/utils/dcimGeometry'

defineOptions({ name: 'RackPage' })

const route = useRoute()
const router = useRouter()
const toast = useToast()
const authStore = useAuthStore()
const canWrite = computed(() => authStore.canWrite)

const loading = ref(true)
const error = ref(null)
const elevation = ref(null)
const face = ref('front')
const selected = ref(null)
const placeForm = ref({ device_id: undefined, offset_u: 1, face: 'front' })
const saving = ref(false)

const rack = computed(() => elevation.value?.rack || {})
const importedHint = computed(() => {
  if (!rack.value.read_only) return ''
  if (rack.value.netbox_id) {
    return 'This rack is imported from NetBox. Placement changes belong in NetBox; Factum only displays them.'
  }
  return 'This rack is imported and read-only.'
})

const unplacedItems = computed(() =>
  (elevation.value?.unplaced || []).map((d) => ({
    label: d.height_ticks
      ? `${d.name} (${d.height_ticks / TICKS_PER_U}U)`
      : `${d.name} (height unknown)`,
    value: d.id,
    disabled: !d.height_ticks,
  })),
)

function errMsg(err, fallback) {
  return err.response?.data?.error ?? fallback
}

function load() {
  loading.value = true
  getRackElevation(route.params.id)
    .then((data) => {
      elevation.value = data
      error.value = null
      if (selected.value) {
        selected.value =
          (data.placements || []).find((p) => p.device_id === selected.value.device_id) || null
      }
    })
    .catch(() => {
      error.value = 'Failed to load rack elevation.'
    })
    .finally(() => {
      loading.value = false
    })
}

function selectPlacement(p) {
  selected.value = p
}

function place() {
  const deviceId = placeForm.value.device_id
  if (!deviceId) {
    toast.add({ color: 'error', title: 'Select a device' })
    return
  }
  const offset = snapOffsetToWholeU((Number(placeForm.value.offset_u) - 1) * TICKS_PER_U)
  saving.value = true
  placeDevice(deviceId, {
    rack_id: rack.value.id,
    offset_ticks: Math.max(0, offset),
    face: placeForm.value.face,
  })
    .then(() => {
      placeForm.value.device_id = undefined
      load()
    })
    .catch((err) => toast.add({ color: 'error', title: 'Place failed', description: errMsg(err) }))
    .finally(() => {
      saving.value = false
    })
}

function unmount() {
  if (!selected.value || selected.value.read_only) return
  saving.value = true
  unmountDevice(selected.value.device_id, selected.value.version)
    .then(() => {
      selected.value = null
      load()
    })
    .catch((err) =>
      toast.add({ color: 'error', title: 'Unmount failed', description: errMsg(err) }),
    )
    .finally(() => {
      saving.value = false
    })
}

function occupancyColor(pct, unknown) {
  if (unknown) return 'warning'
  if (pct >= 80) return 'error'
  if (pct >= 50) return 'primary'
  return 'success'
}

watch(() => route.params.id, load)
onMounted(load)
</script>

<template>
  <div class="flex min-h-0 flex-1 flex-col gap-4 overflow-auto">
    <div class="flex flex-wrap items-center justify-between gap-2">
      <div class="flex items-center gap-2">
        <UButton
          icon="i-lucide-arrow-left"
          variant="ghost"
          color="neutral"
          size="sm"
          @click="router.push('/dcim/racks')"
        />
        <h4 class="m-0">{{ rack.name || 'Rack' }}</h4>
        <UBadge
          v-if="rack.source"
          :label="rack.source"
          :color="rack.source === 'factum' ? 'success' : 'neutral'"
          variant="subtle"
        />
      </div>
      <div class="flex items-center gap-2">
        <UButton
          label="Connections"
          icon="i-lucide-share-2"
          variant="outline"
          color="neutral"
          size="sm"
          @click="router.push({ path: '/dcim/connections', query: { rack_id: rack.id } })"
        />
      </div>
    </div>

    <UAlert
      v-if="importedHint"
      color="neutral"
      variant="subtle"
      :description="importedHint"
      :title="rack.netbox_id ? 'Imported from NetBox' : 'Read-only rack'"
    />

    <div v-if="error" class="text-error">{{ error }}</div>
    <div v-else-if="loading && !elevation" class="text-muted">Loading…</div>
    <div v-else-if="elevation" class="flex flex-wrap gap-6 items-start">
      <div>
        <div class="flex items-center gap-2 mb-2">
          <UButton
            :variant="face === 'front' ? 'solid' : 'outline'"
            size="sm"
            label="Front"
            @click="face = 'front'"
          />
          <UButton
            :variant="face === 'rear' ? 'solid' : 'outline'"
            size="sm"
            label="Rear"
            @click="face = 'rear'"
          />
        </div>
        <RackElevation
          :elevation="elevation"
          :face="face"
          :selected-id="selected?.device_id || 0"
          @select="selectPlacement"
        />
        <div class="mt-3 text-sm flex flex-wrap gap-3">
          <span class="flex items-center gap-1"
            ><span class="inline-block w-3 h-3 bg-slate-200 border border-slate-500"></span>
            Occupied</span
          >
          <span class="flex items-center gap-1"
            ><span class="inline-block w-3 h-3 bg-yellow-300 border border-slate-500"></span> Height
            unknown</span
          >
          <span class="flex items-center gap-1"
            ><span class="inline-block w-3 h-3 bg-red-300 border border-slate-500"></span>
            Conflict</span
          >
          <span class="flex items-center gap-1"
            ><span class="inline-block w-3 h-3 bg-white border border-slate-400"></span> Empty</span
          >
        </div>
      </div>

      <div class="flex min-w-72 flex-1 flex-col gap-4 max-w-lg">
        <div>
          <div class="text-sm text-muted mb-1">Occupancy</div>
          <UBadge
            :label="`${rack.occupancy_percent || 0}%`"
            :color="occupancyColor(rack.occupancy_percent, rack.unknown_dimensions)"
            variant="subtle"
          />
          <span v-if="rack.unknown_dimensions" class="ml-2 text-sm text-warning"
            >{{ rack.unknown_dimensions }} device(s) missing height</span
          >
        </div>

        <div v-if="selected" class="flex flex-col gap-2">
          <h5 class="m-0">{{ selected.device_name }}</h5>
          <div class="text-sm text-muted">
            {{ selected.height_u || '?' }}U · {{ selected.face }}
            <span v-if="selected.full_depth"> · full depth</span>
            <span v-if="selected.read_only"> · imported</span>
          </div>
          <div class="flex gap-2">
            <UButton
              label="Open device"
              size="sm"
              variant="outline"
              @click="router.push({ path: '/device', query: { id: selected.device_id } })"
            />
            <UButton
              v-if="canWrite && !selected.read_only"
              label="Unmount"
              size="sm"
              color="error"
              variant="ghost"
              :loading="saving"
              @click="unmount"
            />
          </div>
          <UAlert
            v-if="selected.read_only"
            color="neutral"
            variant="subtle"
            title="Imported placement"
            description="Change this mount in NetBox, then sync. Factum will not move imported devices."
          />
        </div>

        <div v-if="canWrite && !rack.read_only" class="flex flex-col gap-3">
          <h5 class="m-0">Place a device</h5>
          <UFormField label="Unplaced device">
            <USelect v-model="placeForm.device_id" :items="unplacedItems" class="w-full" />
          </UFormField>
          <UFormField label="Offset from bottom (U)">
            <UInput v-model="placeForm.offset_u" type="number" class="w-full" />
          </UFormField>
          <UFormField label="Face">
            <USelect
              v-model="placeForm.face"
              :items="[
                { label: 'Front', value: 'front' },
                { label: 'Rear', value: 'rear' },
              ]"
              class="w-full"
            />
          </UFormField>
          <UButton label="Place" :loading="saving" @click="place" />
        </div>

        <div v-if="(elevation.accessories || []).length">
          <h5 class="m-0">Zero-U accessories</h5>
          <ul class="text-sm">
            <li v-for="a in elevation.accessories" :key="a.id">{{ a.name }}</li>
          </ul>
        </div>
      </div>
    </div>
  </div>
</template>
