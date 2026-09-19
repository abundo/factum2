<script setup>
import { Handle, Position } from '@vue-flow/core'

defineProps({
  data: { type: Object, required: true },
})
</script>

<template>
  <div
    class="min-w-40 rounded border bg-default px-2 py-1 shadow-sm"
    :class="data.external ? 'border-dashed border-warning' : 'border-default'"
  >
    <div class="text-sm font-medium truncate">{{ data.label }}</div>
    <div v-if="data.external" class="text-xs text-warning">Outside this view</div>
    <div class="mt-1 flex max-h-48 flex-col gap-0.5 overflow-auto">
      <div
        v-for="(iface, i) in data.interfaces || []"
        :key="iface.id"
        class="relative text-[11px] text-muted pr-3"
      >
        {{ iface.name }}
        <Handle
          :id="String(iface.id)"
          type="source"
          :position="Position.Right"
          :style="{ top: `${28 + i * 16}px` }"
        />
        <Handle
          :id="`in-${iface.id}`"
          type="target"
          :position="Position.Left"
          :style="{ top: `${28 + i * 16}px` }"
        />
      </div>
    </div>
  </div>
</template>
