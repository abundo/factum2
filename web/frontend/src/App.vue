<script setup>
import { bindToastToLog } from '@/layout/composables/logPanel'
import { useBranding } from '@/composables/useBranding'

bindToastToLog()
useBranding()
</script>

<template>
  <UApp>
    <router-view />
  </UApp>
</template>
