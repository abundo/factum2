<script setup>
import { Handle, Position } from '@vue-flow/core'

defineProps({
  data: { type: Object, required: true },
})
</script>

<template>
  <div
    class="w-max min-w-40 rounded border bg-default px-2 py-1 shadow-sm"
    :class="data.external ? 'border-dashed border-warning' : 'border-default'"
  >
    <div class="whitespace-nowrap text-sm font-medium">{{ data.label }}</div>
    <div v-if="data.external" class="text-xs text-warning">Outside this view</div>
    <div class="mt-1 flex flex-col">
      <div
        v-for="iface in data.interfaces || []"
        :key="iface.id"
        class="relative flex h-4 items-center text-[11px] leading-none text-muted"
      >
        <Handle
          :id="String(iface.id)"
          type="source"
          :position="Position.Right"
        />
        <Handle
          :id="`in-${iface.id}`"
          type="target"
          :position="Position.Left"
        />
        <span class="whitespace-nowrap">{{ iface.name }}</span>
      </div>
    </div>
  </div>
</template>
