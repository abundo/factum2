<script setup>
import { ref, watch } from 'vue'
import GoTemplateEditor from '@/components/GoTemplateEditor.vue'

defineProps({
  id: { type: String, default: undefined },
  label: { type: String, required: true },
  description: { type: String, default: '' },
  rows: { type: Number, default: 6 },
  placeholder: { type: String, default: '' },
  schema: { type: Object, default: () => ({}) },
})

const model = defineModel({ type: String, default: '' })

const open = ref(false)
const draft = ref('')

watch(open, (isOpen) => {
  if (isOpen) {
    draft.value = model.value ?? ''
  }
})

function apply() {
  model.value = draft.value ?? ''
  open.value = false
}
</script>

<template>
  <div>
    <div class="mb-3 flex items-center justify-between gap-2">
      <label :for="id" class="font-bold">{{ label }}</label>
      <UButton
        size="sm"
        variant="outline"
        color="neutral"
        icon="i-lucide-pencil"
        label="Edit"
        :aria-label="`Edit ${label}`"
        @click="open = true"
      />
    </div>
    <p v-if="description" class="mb-2 text-sm text-muted">{{ description }}</p>
    <UTextarea
      :id="id"
      :model-value="model ?? ''"
      :rows="rows"
      :placeholder="placeholder"
      readonly
      class="w-full font-mono text-sm"
      :ui="{ base: 'bg-muted' }"
    />

    <UModal
      v-model:open="open"
      :title="`Edit ${label}`"
      description="Go text/template"
      :ui="{
        content: 'w-[95vw] h-[90vh] sm:max-w-none flex flex-col bg-default',
        body: 'flex flex-1 min-h-0 flex-col overflow-hidden bg-default',
        footer: 'bg-default',
        header: 'bg-default',
      }"
    >
      <template #body>
        <GoTemplateEditor
          v-if="open"
          v-model="draft"
          class="h-full min-h-0"
          :schema="schema"
          :placeholder="placeholder"
          @apply="apply"
        />
      </template>
      <template #footer>
        <UButton label="Cancel" variant="ghost" @click="open = false" />
        <UButton label="Apply" icon="i-lucide-check" @click="apply" />
      </template>
    </UModal>
  </div>
</template>
