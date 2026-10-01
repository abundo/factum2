<script setup>
import { bindToastToLog } from '@/layout/composables/logPanel'
import { useBranding } from '@/composables/useBranding'
import ConfirmDialog from '@/components/ConfirmDialog.vue'

bindToastToLog()
useBranding()
</script>

<template>
  <UApp>
    <router-view />
    <ConfirmDialog />
  </UApp>
</template>
