<script setup>
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useLayout } from '@/layout/composables/layout'
import { findTemplateIssues, snippetForItem, TEMPLATE_BUILTINS } from '@/utils/goTemplateLanguage'

const props = defineProps({
  schema: { type: Object, default: () => ({}) },
  placeholder: { type: String, default: '' },
})

const model = defineModel({ type: String, default: '' })

const emit = defineEmits(['apply'])

const host = ref(null)
const ready = ref(false)
const { layoutState } = useLayout()

let editor = null

const notes = computed(() => props.schema?.notes || '')
const variables = computed(() => props.schema?.variables ?? [])
const functions = computed(() => props.schema?.functions ?? [])
const issue = computed(() => findTemplateIssues(model.value))

const syntaxHelp = `{{ .Field }}
{{ if .X }} … {{ else }} … {{ end }}
{{ range .Items }} … {{ end }}
{{ func arg }}  {{ .X | func }}
{{- -}}  trims whitespace
{{/* comment */}}`

onMounted(async () => {
  const { createGoTemplateEditor } = await import('@/utils/goTemplateEditor')
  await nextTick()
  if (!host.value) {
    return
  }
  editor = createGoTemplateEditor({
    parent: host.value,
    doc: model.value ?? '',
    schema: props.schema ?? {},
    dark: layoutState.darkTheme,
    placeholder: props.placeholder,
    onChange: (value) => {
      model.value = value
    },
    onApply: () => emit('apply'),
  })
  ready.value = true
  editor.setCursor((model.value ?? '').length)
  editor.focus()
})

watch(
  () => layoutState.darkTheme,
  (dark) => {
    editor?.setDark(dark)
  },
)

watch(model, (value) => {
  if (editor && value !== editor.getValue()) {
    editor.setValue(value ?? '')
  }
})

onBeforeUnmount(() => {
  editor?.destroy()
  editor = null
})

function insertItem(item) {
  editor?.insert(snippetForItem(item))
}

function insertBuiltin(name) {
  editor?.insert(`{{ ${name} }}`)
}
</script>

<template>
  <div class="flex h-full min-h-0 flex-col gap-3 lg:flex-row">
    <div class="flex min-h-0 min-w-0 flex-1 flex-col">
      <div
        ref="host"
        class="min-h-48 flex-1 overflow-hidden rounded-md border border-default bg-muted lg:min-h-64"
        :aria-busy="!ready"
      />
      <p v-if="issue" class="mt-2 text-sm text-red-500">{{ issue }}</p>
      <p v-else class="mt-2 text-xs text-muted">Go text/template. Ctrl+Enter applies.</p>
    </div>

    <aside
      class="flex max-h-48 shrink-0 flex-col overflow-auto rounded-md border border-default p-3 lg:max-h-none lg:w-80"
    >
      <p v-if="notes" class="mb-3 text-sm text-muted">{{ notes }}</p>

      <div class="mb-3">
        <div class="mb-1 text-xs font-semibold tracking-wide text-muted uppercase">Syntax</div>
        <pre class="font-mono text-xs leading-5 text-muted">{{ syntaxHelp }}</pre>
      </div>

      <div v-if="functions.length" class="mb-3">
        <div class="mb-1 text-xs font-semibold tracking-wide text-muted uppercase">Functions</div>
        <button
          v-for="item in functions"
          :key="`${item.name}:${item.args || ''}:${item.insert || ''}`"
          type="button"
          class="block w-full rounded px-1.5 py-1 text-left hover:bg-elevated"
          @click="insertItem(item)"
        >
          <span class="font-mono text-sm">{{ item.name }}</span>
          <span v-if="item.args" class="ml-1 font-mono text-xs text-muted">{{ item.args }}</span>
          <span v-if="item.description" class="block text-xs text-muted">{{
            item.description
          }}</span>
        </button>
      </div>

      <div v-if="variables.length" class="mb-3">
        <div class="mb-1 text-xs font-semibold tracking-wide text-muted uppercase">Variables</div>
        <button
          v-for="item in variables"
          :key="`${item.name}:${item.insert || ''}`"
          type="button"
          class="block w-full rounded px-1.5 py-1 text-left hover:bg-elevated"
          @click="insertItem(item)"
        >
          <span class="font-mono text-sm">{{ item.name }}</span>
          <span v-if="item.type" class="ml-1 text-xs text-muted">{{ item.type }}</span>
          <span v-if="item.description" class="block text-xs text-muted">{{
            item.description
          }}</span>
        </button>
      </div>

      <details>
        <summary class="cursor-pointer text-xs font-semibold tracking-wide text-muted uppercase">
          Built-in functions
        </summary>
        <div class="mt-1 flex flex-wrap gap-1">
          <button
            v-for="name in TEMPLATE_BUILTINS"
            :key="name"
            type="button"
            class="rounded px-1.5 py-0.5 font-mono text-xs hover:bg-elevated"
            @click="insertBuiltin(name)"
          >
            {{ name }}
          </button>
        </div>
      </details>
    </aside>
  </div>
</template>
