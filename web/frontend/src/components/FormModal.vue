<script setup>
import { computed, watch } from 'vue'
import { useToast } from '@nuxt/ui/composables'
import { useFormDirty } from '@/composables/useFormDirty'

defineOptions({ inheritAttrs: false, name: 'FormModal' })

const open = defineModel('open', { type: Boolean })
const props = defineProps({
  source: { default: undefined },
  dirty: { type: Boolean, default: undefined },
  loading: { type: Boolean, default: false },
})

const toast = useToast()
const { dirty: snapshotDirty, markClean } = useFormDirty(() => props.source, {
  open,
  loading: () => props.loading,
})

const isDirty = computed(() => (props.dirty !== undefined ? props.dirty : snapshotDirty.value))

let warned = false
watch(open, (isOpen) => {
  if (isOpen) warned = false
})

function onClosePrevent() {
  if (!isDirty.value || warned) return
  warned = true
  toast.add({
    color: 'warning',
    title: 'Unsaved changes',
    description: 'Click Cancel or Close to discard.',
    duration: 3000,
  })
}

defineExpose({ dirty: isDirty, markClean })
</script>

<template>
  <UModal
    v-bind="$attrs"
    v-model:open="open"
    :dismissible="!isDirty"
    @close:prevent="onClosePrevent"
  >
    <template v-for="(_, name) in $slots" :key="name" #[name]="slotProps">
      <slot :name="name" v-bind="slotProps ?? {}" />
    </template>
  </UModal>
</template>
