<script setup>
import { computed, nextTick, ref, watch } from 'vue'

const props = defineProps({
  devices: { type: Array, default: () => [] },
  sites: { type: Array, default: () => [] },
  selectedId: { type: Number, default: null },
  canWrite: { type: Boolean, default: false },
  picking: { type: Boolean, default: false },
  picked: { type: Object, default: null },
  address: { type: String, default: '' },
  addressLoading: { type: Boolean, default: false },
  saving: { type: Boolean, default: false },
  loading: { type: Boolean, default: false },
})

const emit = defineEmits(['select', 'update:picking', 'assign', 'use-site'])

const search = ref('')
const missingSiteOnly = ref(false)
const siteName = ref('')
const listEl = ref(null)

function hasSite(d) {
  return !!(d?.site && d.site !== 'Default')
}

function formatCoord(lat, lng) {
  if (lat == null || lng == null || Number.isNaN(Number(lat)) || Number.isNaN(Number(lng))) {
    return ''
  }
  return `${Number(lat).toFixed(5)}, ${Number(lng).toFixed(5)}`
}

const filteredDevices = computed(() => {
  const q = search.value.trim().toLowerCase()
  return props.devices.filter((d) => {
    if (d.vm) return false
    if (missingSiteOnly.value && hasSite(d)) return false
    if (!q) return true
    return (d.name || '').toLowerCase().includes(q) || (d.site || '').toLowerCase().includes(q)
  })
})

function byName(a, b) {
  return (a.name || '').localeCompare(b.name || '', undefined, { sensitivity: 'base' })
}

// Named sites A–Z, devices A–Z inside each; unassigned devices last.
const groupedDevices = computed(() => {
  const bySite = new Map()
  const unassigned = []
  for (const d of filteredDevices.value) {
    if (hasSite(d)) {
      if (!bySite.has(d.site)) bySite.set(d.site, [])
      bySite.get(d.site).push(d)
    } else {
      unassigned.push(d)
    }
  }
  const groups = [...bySite.keys()]
    .sort((a, b) => a.localeCompare(b, undefined, { sensitivity: 'base' }))
    .map((name) => ({
      name,
      unassigned: false,
      devices: bySite.get(name).slice().sort(byName),
    }))
  if (unassigned.length) {
    groups.push({
      name: 'No site',
      unassigned: true,
      devices: unassigned.slice().sort(byName),
    })
  }
  return groups
})

const selected = computed(() => props.devices.find((d) => d.id === props.selectedId) ?? null)

const siteItems = computed(() =>
  [...props.sites]
    .slice()
    .sort((a, b) => (a.name || '').localeCompare(b.name || ''))
    .map((s) => ({ label: `${s.name} (${formatCoord(s.latitude, s.longitude)})`, value: s.name })),
)

const namedSite = computed(() => props.sites.find((s) => s.name === siteName.value) ?? null)

const effectiveCoords = computed(() => {
  if (props.picked) return props.picked
  if (namedSite.value) {
    return { lat: namedSite.value.latitude, lng: namedSite.value.longitude }
  }
  if (selected.value?.latitude != null && selected.value?.longitude != null) {
    return { lat: selected.value.latitude, lng: selected.value.longitude }
  }
  return null
})

const canSubmit = computed(() => {
  if (!props.canWrite || !selected.value || props.saving) return false
  if (!selected.value.netbox_id) return false
  return !!effectiveCoords.value
})

watch(
  () => props.selectedId,
  () => {
    const d = selected.value
    siteName.value = hasSite(d) ? d.site : ''
    nextTick(() => {
      const el = listEl.value?.querySelector(`[data-device-id="${props.selectedId}"]`)
      el?.scrollIntoView({ block: 'nearest' })
    })
  },
)

function onExistingSite(name) {
  if (!name) return
  siteName.value = name
  const site = props.sites.find((s) => s.name === name)
  if (site) emit('use-site', site)
}

function submit() {
  if (!canSubmit.value) return
  const coords = effectiveCoords.value
  const payload = {
    latitude: coords.lat,
    longitude: coords.lng,
  }
  const name = siteName.value.trim()
  if (name) payload.site_name = name
  if (name && props.address?.trim()) payload.physical_address = props.address.trim()
  emit('assign', payload)
}
</script>

<template>
  <div class="flex w-80 shrink-0 flex-col min-h-0 rounded-lg border border-default bg-default">
    <div class="p-3 border-b border-default space-y-2 shrink-0">
      <div class="font-medium">Assign locations</div>
      <UInput v-model="search" icon="i-lucide-search" placeholder="Search devices..." size="sm" />
      <label class="flex items-center gap-2 text-sm text-muted-color">
        <UCheckbox v-model="missingSiteOnly" />
        Without a site
      </label>
      <div class="text-xs text-muted-color">
        {{ filteredDevices.length }} of {{ devices.length }} devices
      </div>
    </div>

    <div ref="listEl" class="min-h-0 flex-1 overflow-y-auto">
      <div v-if="loading" class="flex justify-center py-6">
        <UIcon name="i-lucide-loader-2" class="size-5 animate-spin" />
      </div>
      <template v-for="group in groupedDevices" :key="group.name">
        <div
          class="px-3 py-1.5 text-xs font-medium bg-elevated border-b border-default flex items-center justify-between gap-2"
        >
          <span class="truncate" :class="group.unassigned ? 'text-warning' : 'text-muted-color'">
            {{ group.name }}
          </span>
          <span class="text-muted-color font-normal shrink-0">{{ group.devices.length }}</span>
        </div>
        <button
          v-for="d in group.devices"
          :key="d.id"
          type="button"
          :data-device-id="d.id"
          class="w-full text-left px-3 py-2 border-b border-default hover:bg-elevated/60"
          :class="d.id === selectedId ? 'bg-primary/10 border-l-2 border-l-primary' : ''"
          @click="emit('select', d)"
        >
          <div class="font-medium truncate">{{ d.name }}</div>
          <div class="text-xs text-muted-color truncate">
            <template v-if="formatCoord(d.latitude, d.longitude)">
              {{ formatCoord(d.latitude, d.longitude) }}
            </template>
            <template v-else>no coordinates</template>
          </div>
        </button>
      </template>
      <div
        v-if="!loading && !filteredDevices.length"
        class="px-3 py-6 text-sm text-muted-color text-center"
      >
        No matching devices.
      </div>
    </div>

    <div v-if="selected" class="p-3 border-t border-default space-y-3 shrink-0">
      <div>
        <div class="font-medium">{{ selected.name }}</div>
        <div class="text-xs text-muted-color">
          {{ selected.role || 'Unassigned' }} · {{ selected.status || '—' }}
        </div>
      </div>

      <div v-if="!selected.netbox_id" class="text-sm text-muted-color">
        This device is not synced from NetBox, so its location cannot be set here.
      </div>

      <template v-else>
        <div v-if="!hasSite(selected)" class="text-sm">
          Click the map to set coordinates. A site is optional — skip it when this is the only
          device at the location.
        </div>
        <div v-else-if="!formatCoord(selected.latitude, selected.longitude)" class="text-sm">
          Site {{ selected.site }} has no coordinates. Click the map to set them, or clear the site
          name to pin only this device.
        </div>

        <div>
          <label class="block text-xs font-medium mb-1">Site (optional)</label>
          <UInput
            v-model="siteName"
            placeholder="Leave blank to pin this device only"
            size="sm"
            :disabled="!canWrite"
          />
        </div>

        <div v-if="siteItems.length">
          <label class="block text-xs font-medium mb-1">Existing sites</label>
          <USelect
            :model-value="namedSite?.name"
            :items="siteItems"
            value-key="value"
            label-key="label"
            placeholder="Assign to an existing site"
            class="w-full"
            :disabled="!canWrite"
            @update:model-value="onExistingSite"
          />
        </div>

        <div>
          <label class="block text-xs font-medium mb-1">Coordinates</label>
          <div class="text-sm font-mono">
            {{
              effectiveCoords
                ? formatCoord(effectiveCoords.lat, effectiveCoords.lng)
                : 'Not set — click the map'
            }}
          </div>
          <div v-if="addressLoading" class="text-xs text-muted-color mt-1">Looking up address…</div>
          <div v-else-if="address" class="text-xs text-muted-color mt-1 whitespace-pre-line">
            {{ address }}
          </div>
        </div>

        <div class="flex flex-wrap gap-2">
          <UButton
            :label="picking ? 'Picking on map…' : 'Pick on map'"
            icon="i-lucide-map-pin"
            size="xs"
            color="primary"
            :variant="picking ? 'solid' : 'outline'"
            :disabled="!canWrite"
            @click="emit('update:picking', !picking)"
          />
          <UButton
            label="Assign in NetBox"
            icon="i-lucide-check"
            size="xs"
            color="primary"
            :variant="canSubmit ? 'solid' : 'outline'"
            :loading="saving"
            :disabled="!canSubmit"
            @click="submit"
          />
        </div>
      </template>
    </div>
    <div v-else class="p-3 border-t border-default text-sm text-muted-color shrink-0">
      Select a device to pan the map and set its location.
    </div>
  </div>
</template>
