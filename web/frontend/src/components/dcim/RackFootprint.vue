<script setup>
import { computed } from 'vue'
import { rotatedFootprint } from '@/utils/dcimGeometry'

const props = defineProps({
  row: { type: Object, required: true },
})

const fp = computed(() =>
  rotatedFootprint(
    props.row.x_mm,
    props.row.y_mm,
    props.row.rack?.width_mm,
    props.row.rack?.depth_mm,
    props.row.rotation,
  ),
)

const tone = computed(() => {
  const rack = props.row.rack || {}
  if (rack.unknown_dimensions) return 'warning'
  if (rack.has_conflict) return 'error'
  return 'neutral'
})
</script>

<template>
  <div class="flex items-center gap-2 text-sm">
    <UBadge :label="row.rack?.name || `rack ${row.rack_id}`" :color="tone" variant="subtle" />
    <span class="font-mono text-muted">{{ fp.x }},{{ fp.y }} mm · {{ row.rotation }}°</span>
  </div>
</template>
