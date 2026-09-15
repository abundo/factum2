<script setup>
import { computed } from 'vue'
import OxidizedNodePanel from '@/components/OxidizedNodePanel.vue'

const props = defineProps({
  // Already-resolved Oxidized node (Oxidized browser).
  node: { type: Object, default: null },
  // Factum device name to look up when the caller only has a device row
  // (DeviceList). Ignored when `node` is set.
  nodeName: { type: String, default: '' },
})

const open = defineModel('open', { type: Boolean, default: false })

const title = computed(() =>
  props.node?.name ? `${props.node.name} — Oxidized` : 'Oxidized',
)
</script>

<template>
  <UModal
    v-model:open="open"
    :title="title"
    :ui="{ content: 'w-[95vw] h-[90vh] sm:max-w-none' }"
  >
    <template #body>
      <OxidizedNodePanel v-if="open" :node="node" :node-name="nodeName" />
    </template>
    <template #footer>
      <UButton label="Close" icon="i-lucide-x" variant="ghost" @click="open = false" />
    </template>
  </UModal>
</template>
