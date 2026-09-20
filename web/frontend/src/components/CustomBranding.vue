<script setup>
defineProps({
  logo: { type: String, default: '' },
  text: { type: String, default: '' },
  imgClass: { type: String, default: 'h-6 w-auto max-h-8 max-w-40 object-contain' },
  textClass: { type: String, default: 'text-lg font-semibold truncate max-w-48' },
})
</script>

<template>
  <div v-if="logo || text" class="flex min-w-0 items-center gap-2">
    <img v-if="logo" :src="logo" :alt="text || 'Logo'" :class="imgClass" />
    <span v-if="text" :class="textClass">{{ text }}</span>
  </div>
</template>
