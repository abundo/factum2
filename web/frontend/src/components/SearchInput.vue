<script setup>
defineProps({
  placeholder: { type: String, default: 'Search...' },
})
const modelValue = defineModel({ type: String, default: '' })
</script>

<template>
  <UInput
    v-model="modelValue"
    icon="i-lucide-search"
    :placeholder="placeholder"
    :ui="{ trailing: 'pe-1' }"
  >
    <template #trailing>
      <UButton
        v-show="!!modelValue"
        color="neutral"
        variant="link"
        size="sm"
        icon="i-lucide-x"
        aria-label="Clear search"
        @mousedown.prevent
        @click="modelValue = ''"
      />
    </template>
  </UInput>
</template>
