<script setup>
import { nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useLayout } from '@/layout/composables/layout'

const props = defineProps({
  original: { type: String, default: '' },
})

const model = defineModel({ type: String, default: '' })

const host = ref(null)
const ready = ref(false)
const { layoutState } = useLayout()

let editor = null

onMounted(async () => {
  const { createConfigTextEditor } = await import('@/utils/configTextEditor')
  await nextTick()
  if (!host.value) return
  editor = createConfigTextEditor({
    parent: host.value,
    doc: model.value ?? '',
    original: props.original ?? '',
    dark: layoutState.darkTheme,
    onChange: (value) => {
      model.value = value
    },
  })
  ready.value = true
})

watch(
  () => layoutState.darkTheme,
  (dark) => editor?.setDark(dark),
)

watch(model, (value) => {
  if (editor && value !== editor.getValue()) editor.setValue(value ?? '')
})

watch(
  () => props.original,
  (value) => editor?.setOriginal(value ?? ''),
)

onBeforeUnmount(() => {
  editor?.destroy()
  editor = null
})
</script>

<template>
  <div
    ref="host"
    class="min-h-48 w-full flex-1 overflow-hidden bg-muted"
    :aria-busy="!ready"
  />
</template>
