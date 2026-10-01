<script setup>
import { computed, useAttrs } from 'vue'
import { useFormDirty } from '@/composables/useFormDirty'
import { useConfirm } from '@/composables/useConfirm'
import { useUnsaved } from '@/composables/useFormGuard'
import { wideModal } from '@/utils/form'

defineOptions({ inheritAttrs: false, name: 'FormModal' })

const open = defineModel('open', { type: Boolean })
const props = defineProps({
  source: { default: undefined },
  dirty: { type: Boolean, default: undefined },
  loading: { type: Boolean, default: false },
})

const attrs = useAttrs()
const { ask } = useConfirm()
const { dirty: snapshotDirty, markClean } = useFormDirty(() => props.source, {
  open,
  loading: () => props.loading,
})

const isDirty = computed(() => (props.dirty !== undefined ? props.dirty : snapshotDirty.value))

// Save sets `open` false directly. Cancel and the modal X call requestClose,
// which asks before discarding. The footer slot receives `close`.
useUnsaved(
  () => !!open.value && isDirty.value,
  () => {
    open.value = false
  },
)

const modalUi = computed(() => {
  const passed = attrs.ui && typeof attrs.ui === 'object' ? attrs.ui : {}
  const content = [wideModal.content, passed.content].filter(Boolean).join(' ')
  return { ...passed, content }
})

const restAttrs = computed(() => {
  const rest = { ...attrs }
  delete rest.ui
  return rest
})

async function requestClose() {
  if (!open.value) return
  if (isDirty.value) {
    const yes = await ask({
      title: 'Unsaved changes',
      message: 'You have changes that are not saved. Discard them?',
    })
    if (!yes) return
  }
  open.value = false
}

function onUpdateOpen(next) {
  if (next) open.value = true
  else requestClose()
}

defineExpose({ dirty: isDirty, markClean, requestClose })
</script>

<template>
  <UModal
    v-bind="restAttrs"
    :open="open"
    :ui="modalUi"
    :dismissible="false"
    @update:open="onUpdateOpen"
  >
    <template v-for="(_, name) in $slots" :key="name" #[name]="slotProps">
      <slot
        :name="name"
        v-bind="name === 'footer' ? { ...(slotProps ?? {}), close: requestClose } : (slotProps ?? {})"
      />
    </template>
  </UModal>
</template>
