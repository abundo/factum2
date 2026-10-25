<script setup>
import PageFormBar from '@/components/PageFormBar.vue'
import { useSettings } from '@/composables/useSettings'

defineProps({
  title: { type: String, required: true },
})

const { settings, loading, saving, forbidden, loadError, save } = useSettings()
</script>

<template>
  <div v-if="forbidden" class="card">
    <UAlert
      color="error"
      variant="subtle"
      title="You need administrator permissions to view settings."
    />
  </div>
  <div v-else-if="loadError" class="card">
    <UAlert color="error" variant="subtle" title="Failed to load settings." />
  </div>
  <div v-else class="card">
    <PageFormBar page :title="title">
      <UButton
        label="Save"
        icon="i-lucide-check"
        :loading="saving"
        :disabled="loading"
        @click="save"
      />
    </PageFormBar>

    <div v-if="loading" class="flex justify-center p-4">
      <UIcon name="i-lucide-loader-2" class="size-8 animate-spin" />
    </div>

    <div v-else class="flex flex-col gap-6">
      <slot :settings="settings" />
    </div>
  </div>
</template>
