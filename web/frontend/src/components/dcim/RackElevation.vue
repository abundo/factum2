<script setup>
import { computed } from 'vue'
import RackDevice from '@/components/dcim/RackDevice.vue'
import { TICKS_PER_U } from '@/utils/dcimGeometry'

const props = defineProps({
  elevation: { type: Object, required: true },
  face: { type: String, default: 'front' },
  selectedId: { type: Number, default: 0 },
  ghost: { type: Object, default: null },
  ghostInvalid: { type: Boolean, default: false },
})

const emit = defineEmits(['select', 'drop-offset'])

const rack = computed(() => props.elevation.rack || {})
const heightU = computed(() => rack.value.height_u || 42)
const rackHeightTicks = computed(() => heightU.value * TICKS_PER_U)
const pxPerU = 16
const pxPerTick = computed(() => pxPerU / TICKS_PER_U)
const railWidth = 28
const chassisWidth = 220
const svgWidth = railWidth + chassisWidth + 8
const svgHeight = computed(() => heightU.value * pxPerU + 8)

const labels = computed(() => props.elevation.unit_labels || [])

const visible = computed(() => {
  const rows = props.elevation.placements || []
  return rows.filter((p) => {
    if (p.zero_u) return false
    if (p.full_depth) return true
    return (p.face || 'front') === props.face
  })
})

function yToOffset(clientY, svgEl) {
  const rect = svgEl.getBoundingClientRect()
  const y = clientY - rect.top - 4
  const ticksFromTop = y / pxPerTick.value
  return Math.round(rackHeightTicks.value - ticksFromTop)
}

function onClick(ev) {
  emit('drop-offset', yToOffset(ev.clientY, ev.currentTarget))
}
</script>

<template>
  <svg
    class="border border-default rounded bg-default select-none"
    :width="svgWidth"
    :height="svgHeight"
    :viewBox="`0 0 ${svgWidth} ${svgHeight}`"
    @click="onClick"
  >
    <rect x="0" y="4" :width="railWidth" :height="heightU * pxPerU" fill="rgb(241 245 249)" />
    <g v-for="u in labels" :key="u.offset_u">
      <line
        :x1="0"
        :x2="railWidth + chassisWidth"
        :y1="4 + (heightU - 1 - u.offset_u) * pxPerU"
        :y2="4 + (heightU - 1 - u.offset_u) * pxPerU"
        stroke="rgb(226 232 240)"
      />
      <text
        :x="railWidth - 4"
        :y="4 + (heightU - 1 - u.offset_u) * pxPerU + 14"
        font-size="10"
        text-anchor="end"
        fill="rgb(71 85 105)"
      >
        {{ u.label }}
      </text>
    </g>
    <rect
      :x="railWidth"
      y="4"
      :width="chassisWidth"
      :height="heightU * pxPerU"
      fill="rgb(248 250 252)"
      stroke="rgb(148 163 184)"
    />
    <RackDevice
      v-for="p in visible"
      :key="p.id"
      :placement="p"
      :rack-height-ticks="rackHeightTicks"
      :px-per-tick="pxPerTick"
      :width="chassisWidth - 4"
      :x="railWidth + 2"
      :selected="selectedId === p.device_id"
      @select="emit('select', $event)"
    />
    <RackDevice
      v-if="ghost"
      :placement="ghost"
      :rack-height-ticks="rackHeightTicks"
      :px-per-tick="pxPerTick"
      :width="chassisWidth - 4"
      :x="railWidth + 2"
      ghost
      :invalid="ghostInvalid"
    />
  </svg>
</template>
