<script setup>
import { useToast } from '@nuxt/ui/composables'
import { computed, ref, watch } from 'vue'
import { exportConfigBundle, importConfigBundle } from '@/api/config'

const props = defineProps({
  open: { type: Boolean, default: false },
  mode: { type: String, default: 'export' },
  canWrite: { type: Boolean, default: false },
  serviceTypes: { type: Array, default: () => [] },
  macros: { type: Array, default: () => [] },
  scopes: { type: Array, default: () => [] },
})

const emit = defineEmits(['update:open', 'imported'])

const toast = useToast()
const saving = ref(false)
const error = ref('')
const related = ref(true)
const includeSecrets = ref(false)
const pickedTypes = ref([])
const pickedCLI = ref([])
const pickedMacros = ref([])
const pickedParameters = ref([])
const importBody = ref(null)
const fileInput = ref(null)

const title = computed(() =>
  props.mode === 'import' ? 'Import definitions' : 'Export definitions',
)

function scopePath(scope) {
  const byID = new Map(props.scopes.map((s) => [s.id, s]))
  const parts = []
  const seen = new Set()
  let cur = scope
  while (cur) {
    if (seen.has(cur.id)) return ''
    seen.add(cur.id)
    if (String(cur.name ?? '').includes('/')) return ''
    parts.unshift(cur.name)
    if (!cur.parent_id) break
    cur = byID.get(cur.parent_id)
    if (!cur) return ''
  }
  return parts.join('/')
}

const cliOptions = computed(() =>
  props.scopes
    .filter((s) => s.kind === 'cli')
    .map((s) => {
      const path = scopePath(s)
      const st = props.serviceTypes.find((t) => t.id === s.service_type_id)
      const detail = [st?.name, s.platform].filter(Boolean).join(' · ')
      return { path, detail }
    })
    .filter((row) => row.path)
    .sort((a, b) => a.path.localeCompare(b.path)),
)

const parameterOptions = computed(() =>
  props.scopes
    .filter((s) => s.kind === 'parameter')
    .map((s) => ({ path: scopePath(s) }))
    .filter((row) => row.path)
    .sort((a, b) => a.path.localeCompare(b.path)),
)

const macroOptions = computed(() =>
  [...props.macros]
    .map((m) => m.name)
    .filter(Boolean)
    .sort((a, b) => a.localeCompare(b)),
)

const typeOptions = computed(() =>
  [...props.serviceTypes]
    .map((t) => t.name)
    .filter(Boolean)
    .sort((a, b) => a.localeCompare(b)),
)

const pickedCount = computed(
  () =>
    pickedTypes.value.length +
    pickedCLI.value.length +
    pickedMacros.value.length +
    pickedParameters.value.length,
)

const importSummary = computed(() => {
  const b = importBody.value
  if (!b || typeof b !== 'object') return null
  const names = (key, field) =>
    Array.isArray(b[key]) ? b[key].map((row) => row?.[field]).filter(Boolean) : []
  return [
    ['Service definitions', names('service_definitions', 'name')],
    ['CLI objects', names('cli_objects', 'path')],
    ['Macros', names('macros', 'name')],
    ['Parameter objects', names('parameter_objects', 'path')],
    ['Variables', names('variables', 'name')],
  ]
})

watch(
  () => props.open,
  (open) => {
    if (!open) return
    error.value = ''
    saving.value = false
    importBody.value = null
    const types = new Set(typeOptions.value)
    const clis = new Set(cliOptions.value.map((row) => row.path))
    const macros = new Set(macroOptions.value)
    const params = new Set(parameterOptions.value.map((row) => row.path))
    pickedTypes.value = pickedTypes.value.filter((name) => types.has(name))
    pickedCLI.value = pickedCLI.value.filter((path) => clis.has(path))
    pickedMacros.value = pickedMacros.value.filter((name) => macros.has(name))
    pickedParameters.value = pickedParameters.value.filter((path) => params.has(path))
  },
)

function close() {
  emit('update:open', false)
}

function errMsg(err, fallback) {
  return err.response?.data?.error ?? err.message ?? fallback
}

const lists = {
  types: pickedTypes,
  cli: pickedCLI,
  macros: pickedMacros,
  parameters: pickedParameters,
}

function toggle(key, value, on) {
  const list = lists[key]
  const next = new Set(list.value)
  if (on) next.add(value)
  else next.delete(value)
  list.value = [...next]
}

function setAll(key, on) {
  const values = {
    types: typeOptions.value,
    cli: cliOptions.value.map((row) => row.path),
    macros: macroOptions.value,
    parameters: parameterOptions.value.map((row) => row.path),
  }[key]
  lists[key].value = on ? [...values] : []
}

function downloadName() {
  if (pickedTypes.value.length === 1) {
    const safe = pickedTypes.value[0].replace(/[^\w.-]+/g, '_')
    return `${safe}.factum.json`
  }
  return 'factum-config.json'
}

async function doExport() {
  error.value = ''
  saving.value = true
  try {
    const bundle = await exportConfigBundle({
      service_definitions: pickedTypes.value,
      cli_objects: pickedCLI.value,
      macros: pickedMacros.value,
      parameter_objects: pickedParameters.value,
      related: related.value,
      include_secrets: includeSecrets.value,
    })
    const blob = new Blob([JSON.stringify(bundle, null, 2) + '\n'], { type: 'application/json' })
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = downloadName()
    a.click()
    URL.revokeObjectURL(url)
    const parts = [
      bundle.service_definitions?.length &&
        `${bundle.service_definitions.length} service definition${bundle.service_definitions.length === 1 ? '' : 's'}`,
      bundle.cli_objects?.length &&
        `${bundle.cli_objects.length} CLI object${bundle.cli_objects.length === 1 ? '' : 's'}`,
      bundle.macros?.length &&
        `${bundle.macros.length} macro${bundle.macros.length === 1 ? '' : 's'}`,
      bundle.parameter_objects?.length &&
        `${bundle.parameter_objects.length} parameter object${bundle.parameter_objects.length === 1 ? '' : 's'}`,
    ].filter(Boolean)
    toast.add({
      color: 'success',
      title: 'Exported',
      description: parts.length ? parts.join(', ') : 'Empty bundle',
    })
    close()
  } catch (err) {
    error.value = errMsg(err, 'Export failed.')
  } finally {
    saving.value = false
  }
}

function onFile(event) {
  const file = event.target.files?.[0]
  event.target.value = ''
  importBody.value = null
  error.value = ''
  if (!file) return
  const reader = new FileReader()
  reader.onload = () => {
    try {
      const data = JSON.parse(String(reader.result ?? ''))
      if (!data || data.format !== 'factum2-config') {
        throw new Error('This is not a Factum config file.')
      }
      importBody.value = data
    } catch (err) {
      error.value = err.message || 'Could not read the file.'
    }
  }
  reader.readAsText(file)
}

async function doImport() {
  if (!importBody.value) return
  error.value = ''
  saving.value = true
  try {
    const res = await importConfigBundle(importBody.value)
    const created = res?.created?.length ?? 0
    const updated = res?.updated?.length ?? 0
    const warnings = res?.warnings ?? []
    toast.add({
      color: warnings.length ? 'warning' : 'success',
      title: 'Imported',
      description: [
        `${created} created`,
        `${updated} updated`,
        warnings.length ? `${warnings.length} warning${warnings.length === 1 ? '' : 's'}` : '',
      ]
        .filter(Boolean)
        .join(', '),
    })
    if (warnings.length) error.value = warnings.join('\n')
    emit('imported')
    if (!warnings.length) close()
  } catch (err) {
    error.value = errMsg(err, 'Import failed.')
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <UModal
    :open="open"
    :title="title"
    :ui="{ content: 'max-w-2xl w-full' }"
    @update:open="(v) => emit('update:open', v)"
  >
    <template #body>
      <div v-if="mode === 'export'" class="flex flex-col gap-4">
        <p class="text-muted-color text-sm m-0">
          Download service definitions, CLI objects, macros, and parameter objects as one JSON file.
          Check a definition such as ELINE and leave related objects on to include its translation
          CLI, the macros those templates include, and the parameter objects that assign variables
          the templates read. Variable definitions travel with that selection.
        </p>
        <label class="flex items-start gap-2 text-sm">
          <UCheckbox v-model="related" />
          <span>Include related objects</span>
        </label>
        <label class="flex items-start gap-2 text-sm">
          <UCheckbox v-model="includeSecrets" />
          <span>Include secret values. Otherwise import keeps secrets already stored.</span>
        </label>

        <section class="flex flex-col gap-2">
          <div class="flex items-center justify-between gap-2">
            <h6 class="m-0">Service definitions</h6>
            <UButton
              size="xs"
              variant="ghost"
              color="neutral"
              label="All"
              @click="setAll('types', true)"
            />
          </div>
          <p v-if="!typeOptions.length" class="text-muted-color text-sm m-0">None.</p>
          <div v-else class="flex max-h-36 flex-col gap-1 overflow-auto">
            <label v-for="name in typeOptions" :key="name" class="flex items-center gap-2 text-sm">
              <UCheckbox
                :model-value="pickedTypes.includes(name)"
                @update:model-value="(v) => toggle('types', name, v)"
              />
              <span>{{ name }}</span>
            </label>
          </div>
        </section>

        <section class="flex flex-col gap-2">
          <div class="flex items-center justify-between gap-2">
            <h6 class="m-0">CLI objects</h6>
            <UButton
              size="xs"
              variant="ghost"
              color="neutral"
              label="All"
              @click="setAll('cli', true)"
            />
          </div>
          <p v-if="!cliOptions.length" class="text-muted-color text-sm m-0">None in the tree.</p>
          <div v-else class="flex max-h-36 flex-col gap-1 overflow-auto">
            <label v-for="row in cliOptions" :key="row.path" class="flex items-start gap-2 text-sm">
              <UCheckbox
                :model-value="pickedCLI.includes(row.path)"
                @update:model-value="(v) => toggle('cli', row.path, v)"
              />
              <span class="break-all font-mono">
                {{ row.path }}
                <span v-if="row.detail" class="text-muted-color font-sans">
                  · {{ row.detail }}</span
                >
              </span>
            </label>
          </div>
        </section>

        <section class="flex flex-col gap-2">
          <div class="flex items-center justify-between gap-2">
            <h6 class="m-0">Macros</h6>
            <UButton
              size="xs"
              variant="ghost"
              color="neutral"
              label="All"
              @click="setAll('macros', true)"
            />
          </div>
          <p v-if="!macroOptions.length" class="text-muted-color text-sm m-0">None.</p>
          <div v-else class="flex max-h-36 flex-col gap-1 overflow-auto">
            <label v-for="name in macroOptions" :key="name" class="flex items-center gap-2 text-sm">
              <UCheckbox
                :model-value="pickedMacros.includes(name)"
                @update:model-value="(v) => toggle('macros', name, v)"
              />
              <span>{{ name }}</span>
            </label>
          </div>
        </section>

        <section class="flex flex-col gap-2">
          <div class="flex items-center justify-between gap-2">
            <h6 class="m-0">Parameter objects</h6>
            <UButton
              size="xs"
              variant="ghost"
              color="neutral"
              label="All"
              @click="setAll('parameters', true)"
            />
          </div>
          <p v-if="!parameterOptions.length" class="text-muted-color text-sm m-0">
            None in the tree.
          </p>
          <div v-else class="flex max-h-36 flex-col gap-1 overflow-auto">
            <label
              v-for="row in parameterOptions"
              :key="row.path"
              class="flex items-start gap-2 text-sm"
            >
              <UCheckbox
                :model-value="pickedParameters.includes(row.path)"
                @update:model-value="(v) => toggle('parameters', row.path, v)"
              />
              <span class="break-all font-mono">{{ row.path }}</span>
            </label>
          </div>
        </section>
      </div>

      <div v-else class="flex flex-col gap-3">
        <p class="text-muted-color text-sm m-0">
          Objects with the same name or path are updated. CLI features, connection types, and
          parameter assignments in the file replace what is stored on those objects. Nothing else is
          deleted. Missing folders on a path are created. A device or service named in the path must
          already be in the tree.
        </p>
        <div>
          <input
            ref="fileInput"
            type="file"
            accept="application/json,.json"
            class="hidden"
            @change="onFile"
          />
          <UButton
            icon="i-lucide-file-json"
            variant="outline"
            color="neutral"
            label="Choose file"
            @click="fileInput?.click()"
          />
        </div>
        <ul v-if="importSummary" class="m-0 flex list-none flex-col gap-2 p-0 text-sm">
          <li v-for="[label, names] in importSummary" :key="label">
            <span class="font-bold">{{ label }}</span>
            ({{ names.length }})
            <span v-if="names.length" class="text-muted-color break-all">{{
              names.join(', ')
            }}</span>
          </li>
        </ul>
      </div>

      <p v-if="error" class="text-error text-sm whitespace-pre-wrap mt-3 mb-0">{{ error }}</p>
    </template>
    <template #footer>
      <UButton label="Cancel" variant="ghost" @click="close" />
      <UButton
        v-if="mode === 'export'"
        label="Export"
        icon="i-lucide-download"
        :disabled="!pickedCount"
        :loading="saving"
        @click="doExport"
      />
      <UButton
        v-else-if="canWrite"
        label="Import"
        icon="i-lucide-upload"
        :disabled="!importBody"
        :loading="saving"
        @click="doImport"
      />
    </template>
  </UModal>
</template>
