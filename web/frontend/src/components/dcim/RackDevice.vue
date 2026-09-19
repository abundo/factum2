<script setup>
import { computed } from 'vue'
import { offsetTicksToYFromTop, ticksToU } from '@/utils/dcimGeometry'

const props = defineProps({
  placement: { type: Object, required: true },
  rackHeightTicks: { type: Number, required: true },
  pxPerTick: { type: Number, required: true },
  width: { type: Number, required: true },
  x: { type: Number, default: 0 },
  selected: { type: Boolean, default: false },
  ghost: { type: Boolean, default: false },
  invalid: { type: Boolean, default: false },
})

const emit = defineEmits(['select'])

const box = computed(() => {
  const h = props.placement.height_ticks || 0
  const height = Math.max(h, 1) * props.pxPerTick
  const y = offsetTicksToYFromTop(
    props.placement.offset_ticks || 0,
    h || 1,
    props.rackHeightTicks,
    props.pxPerTick,
  )
  return { y, height }
})

const fill = computed(() => {
  if (props.invalid) return 'rgb(254 202 202)'
  if (props.ghost) return 'rgb(191 219 254 / 0.7)'
  if (props.placement.unknown_height) return 'rgb(253 224 71)'
  if (props.placement.conflict) return 'rgb(252 165 165)'
  if (props.selected) return 'rgb(147 197 253)'
  return 'rgb(226 232 240)'
})

const label = computed(() => {
  const name = props.placement.device_name || `device ${props.placement.device_id}`
  const u = ticksToU(props.placement.height_ticks || 0)
  return u ? `${name} (${u}U)` : name
})
</script>

<template>
  <g class="cursor-pointer" :opacity="ghost ? 0.8 : 1" @click.stop="emit('select', placement)">
    <rect
      :x="x"
      :y="box.y"
      :width="width"
      :height="box.height"
      :fill="fill"
      :stroke="selected ? 'rgb(37 99 235)' : 'rgb(71 85 105)'"
      stroke-width="1"
      rx="2"
    />
    <text
      :x="x + 6"
      :y="box.y + Math.min(14, box.height - 2)"
      font-size="11"
      fill="rgb(15 23 42)"
      class="pointer-events-none"
    >
      {{ label }}
    </text>
  </g>
</template>
