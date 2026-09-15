<script setup>
defineOptions({ name: 'IpamPickerTree' })

defineProps({
  nodes: { type: Array, default: () => [] },
  expanded: { type: Object, default: () => ({}) },
  selectedKey: { type: String, default: '' },
})
const emit = defineEmits(['toggle', 'select'])
</script>

<template>
  <ul>
    <li v-for="node in nodes" :key="node.key">
      <button
        type="button"
        class="flex w-full items-center gap-1 rounded px-1 py-0.5 text-left"
        :class="selectedKey === node.key ? 'bg-primary/20' : 'hover:bg-elevated'"
        @click="emit('select', node)"
      >
        <span
          v-if="node.lazy || (node.children && node.children.length)"
          class="w-4 shrink-0 text-center"
          @click.stop="emit('toggle', node)"
        >
          {{ expanded[node.key] ? '−' : '+' }}
        </span>
        <span v-else class="w-4 shrink-0" />
        <span>{{ node.title }}</span>
      </button>
      <div v-if="expanded[node.key] && node.children?.length" class="ml-3">
        <IpamPickerTree
          :nodes="node.children"
          :expanded="expanded"
          :selected-key="selectedKey"
          @toggle="emit('toggle', $event)"
          @select="emit('select', $event)"
        />
      </div>
    </li>
  </ul>
</template>
