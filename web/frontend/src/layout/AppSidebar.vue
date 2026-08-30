<script setup>
import AppMenu from './AppMenu.vue'
import BuildInfo from '@/components/BuildInfo.vue'
</script>

<template>
  <aside
    class="hidden lg:flex lg:h-full lg:w-64 lg:shrink-0 lg:flex-col border-r border-default bg-default p-4"
  >
    <div class="min-h-0 flex-1 overflow-y-auto">
      <AppMenu />
    </div>
    <BuildInfo class="mt-3 shrink-0 border-t border-default pt-3" />
  </aside>
</template>
