<script setup>
import { bindToastToLog } from '@/layout/composables/logPanel'

bindToastToLog()
</script>

<template>
  <UApp>
    <router-view />
  </UApp>
</template>
