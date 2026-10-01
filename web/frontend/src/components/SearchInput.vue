<script setup>
// Search field above a table (AGENTS.md, GUI design rules). The X clears
// it, and so does Escape. Enter does not submit a form around it.
const modelValue = defineModel({ type: String, default: '' })
defineProps({
  placeholder: { type: String, default: 'Search...' },
  size: { type: String, default: 'md' },
})
</script>

<template>
  <UInput
    v-model="modelValue"
    icon="i-lucide-search"
    :placeholder="placeholder"
    :aria-label="placeholder"
    :size="size"
    :ui="{ trailing: 'pe-1' }"
    @keydown.enter.prevent
    @keydown.esc="modelValue = ''"
  >
    <template v-if="modelValue" #trailing>
      <UButton
        color="neutral"
        variant="link"
        :size="size"
        icon="i-lucide-x"
        aria-label="Clear search"
        title="Clear search"
        @mousedown.prevent
        @click="modelValue = ''"
      />
    </template>
  </UInput>
</template>
