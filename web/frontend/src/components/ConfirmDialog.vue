<script setup>
// ConfirmDialog: the Yes/No question of useConfirm. Mounted once in App.vue.
// No has the focus, so Enter doesn't delete by accident. The z-index keeps
// this one above a form's modal.
import { useConfirm } from '@/composables/useConfirm'

const { state, done } = useConfirm()
const ui = { overlay: 'z-[60]', content: 'z-[60]' }
</script>

<template>
  <UModal
    :open="state.open"
    :title="state.title"
    :ui="ui"
    :dismissible="false"
    @update:open="(o) => o || done(false)"
  >
    <template #body>
      <p class="text-sm">{{ state.message }}</p>
      <p v-if="state.detail" class="mt-2 text-sm text-muted">{{ state.detail }}</p>
    </template>
    <template #footer>
      <div class="flex w-full justify-end gap-2">
        <UButton color="neutral" variant="outline" autofocus @click="done(false)">No</UButton>
        <UButton color="error" @click="done(true)">Yes</UButton>
      </div>
    </template>
  </UModal>
</template>
