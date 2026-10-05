<script setup>
import { onMounted, ref } from 'vue'
import CapturePage from '@/views/capture/CapturePage.vue'

// The packet capture alone in a popup window (no menu or top bar). The
// device and interface come from ?device=&interface=, and live only in
// this window.
const loaded = ref(false)

onMounted(() => {
  loaded.value = true
  document.title = 'Packet capture · Factum'
})
</script>

<template>
  <div class="flex h-screen flex-col gap-2 bg-default p-3">
    <CapturePage v-if="loaded" in-window class="min-h-0 flex-1" />
  </div>
</template>
