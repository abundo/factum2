<script setup>
import { useRoute } from 'vue-router'

const route = useRoute()
</script>

<template>
  <div class="card">
    <div class="font-semibold text-xl mb-4">{{ route.meta.title }}</div>
    <p class="text-muted-color">This page hasn't been built yet.</p>
  </div>
</template>
