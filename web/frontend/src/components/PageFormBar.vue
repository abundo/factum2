<script setup>
defineProps({
  title: { type: String, default: '' },
  // Cancel the content padding on the scrolling main area so the bar stays
  // flush with the top of the page while the form below it scrolls.
  page: { type: Boolean, default: false },
})
</script>

<template>
  <div
    class="sticky top-0 z-20 flex flex-wrap items-center justify-between gap-3 bg-default"
    :class="page ? '-mx-4 mb-4 px-4 py-3 md:-mx-6 md:px-6' : 'mb-3 py-2'"
  >
    <div class="min-w-0 text-lg font-semibold">
      <slot name="title">{{ title }}</slot>
    </div>
    <div class="flex shrink-0 flex-wrap items-center justify-end gap-2">
      <slot />
    </div>
  </div>
</template>
