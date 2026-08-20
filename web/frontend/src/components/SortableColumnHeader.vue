<script setup>
defineProps({
  column: { type: Object, required: true },
  label: { type: String, required: true },
})
</script>

<template>
  <UButton
    :label="label"
    color="neutral"
    variant="ghost"
    :icon="
      column.getIsSorted() === 'asc'
        ? 'i-lucide-arrow-up'
        : column.getIsSorted() === 'desc'
          ? 'i-lucide-arrow-down'
          : 'i-lucide-arrow-up-down'
    "
    class="-mx-2.5"
    @click="column.toggleSorting(column.getIsSorted() === 'asc')"
  />
</template>
