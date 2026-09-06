<script setup>
import { computed, ref, watch } from 'vue'
import { useToast } from '@nuxt/ui/composables'
import {
  createFeature,
  deleteFeature,
  listFeatures,
  updateFeature,
  updateScope,
} from '@/api/config'
import GoTemplateEditor from '@/components/GoTemplateEditor.vue'
import {
  cfgmgmtBaselineSchema,
  cfgmgmtPackSchema,
  withCfgmgmtContext,
} from '@/utils/goTemplateSchemas'

defineOptions({ name: 'ConfigNodeInspector' })

const props = defineProps({
  selected: { type: Object, default: null },
  assignments: { type: Array, default: () => [] },
  resolved: { type: Array, default: () => [] },
  variables: { type: Array, default: () => [] },
  serviceTypes: { type: Array, default: () => [] },
  macros: { type: Array, default: () => [] },
  canWrite: { type: Boolean, default: false },
})

const emit = defineEmits(['assign', 'delete-assignment', 'saved'])

const toast = useToast()
const saving = ref(false)
const features = ref([])
const openFeatureId = ref(null)
const cliForm = ref(emptyCLIForm())
const featureDrafts = ref({})

const platformOptions = [
  { label: '(all)', value: '' },
  { label: 'eos', value: 'eos' },
  { label: 'ios-xr', value: 'ios-xr' },
  { label: 'sros', value: 'sros' },
  { label: 'sros-md', value: 'sros-md' },
  { label: 'vrp', value: 'vrp' },
]
const payloadKindOptions = [
  { label: 'cli', value: 'cli' },
  { label: 'netconf', value: 'netconf' },
  { label: 'restconf', value: 'restconf' },
]
const enterPlaceholder = 'interface {{.LocalIface}}'

const typeOptions = computed(() => [
  { label: '(baseline)', value: null },
  ...props.serviceTypes.map((t) => ({ label: t.name, value: t.id })),
])

const isCLI = computed(() => props.selected?.kind === 'cli')
const showAssignments = computed(() => !!props.selected && !isCLI.value)

const cliSchema = computed(() => {
  const typeId = optionValue(cliForm.value.service_type_id)
  const serviceType = props.serviceTypes.find((t) => t.id === typeId) ?? null
  const base = serviceType ? cfgmgmtPackSchema : cfgmgmtBaselineSchema
  return withCfgmgmtContext(base, {
    macros: props.macros,
    variables: props.variables,
    serviceType,
  })
})

function emptyCLIForm() {
  return {
    platform: '',
    payload_kind: 'cli',
    enabled: true,
    service_type_id: null,
    description: '',
    pattern: '',
    enter: '',
    exit: '',
  }
}

function optionValue(v) {
  if (v && typeof v === 'object' && !Array.isArray(v) && 'value' in v) return v.value
  return v
}

function errMsg(err, fallback) {
  return err.response?.data?.error ?? fallback
}

function assignmentName(id) {
  return props.variables.find((v) => v.id === id)?.name ?? `#${id}`
}

function resetCLIForm(node) {
  const ctx = node?.payload?.context ?? {}
  cliForm.value = {
    platform: node?.platform ?? '',
    payload_kind: node?.payload_kind || 'cli',
    enabled: node?.enabled !== false,
    service_type_id: node?.service_type_id ?? null,
    description: node?.payload?.description ?? '',
    pattern: ctx.pattern ?? '',
    enter: ctx.enter ?? '',
    exit: ctx.exit ?? '',
  }
}

async function loadFeatures(scopeId) {
  if (!scopeId) {
    features.value = []
    return
  }
  try {
    features.value = (await listFeatures(scopeId)) ?? []
  } catch (err) {
    features.value = []
    toast.add({
      color: 'error',
      title: 'Error',
      description: errMsg(err, 'Failed to load features.'),
    })
  }
  const drafts = {}
  for (const f of features.value) {
    drafts[f.id] = {
      name: f.name,
      sort_order: f.sort_order,
      add_commands: f.add_commands ?? '',
      update_commands: f.update_commands ?? '',
      remove_commands: f.remove_commands ?? '',
      remove_at_root: !!f.remove_at_root,
    }
  }
  featureDrafts.value = drafts
  if (openFeatureId.value && !drafts[openFeatureId.value]) {
    openFeatureId.value = features.value[0]?.id ?? null
  }
}

watch(
  () => props.selected,
  (node) => {
    if (node?.kind === 'cli') {
      resetCLIForm(node)
      loadFeatures(node.id)
    } else {
      features.value = []
      featureDrafts.value = {}
      openFeatureId.value = null
    }
  },
  { immediate: true },
)

function saveCLI() {
  if (!props.selected?.id) return
  saving.value = true
  const typeId = optionValue(cliForm.value.service_type_id)
  const pattern = (cliForm.value.pattern ?? '').trim()
  const enter = (cliForm.value.enter ?? '').trim()
  const exit = (cliForm.value.exit ?? '').trim()
  const prev = props.selected?.payload ?? {}
  const merged = { ...prev, description: cliForm.value.description ?? '' }
  if (pattern || enter || exit) {
    merged.context = {
      ...(prev.context ?? {}),
      pattern,
      enter,
      exit,
    }
  } else {
    delete merged.context
  }
  const payload = {
    platform: optionValue(cliForm.value.platform) ?? '',
    payload_kind: optionValue(cliForm.value.payload_kind) || 'cli',
    enabled: !!cliForm.value.enabled,
    service_type_id: typeId || 0,
    payload: merged,
  }
  updateScope(props.selected.id, payload)
    .then(() => {
      toast.add({ color: 'success', title: 'Successful', description: 'CLI object saved' })
      emit('saved')
    })
    .catch((err) =>
      toast.add({ color: 'error', title: 'Error', description: errMsg(err, 'Save failed.') }),
    )
    .finally(() => {
      saving.value = false
    })
}

function addFeature() {
  if (!props.selected?.id) return
  const name = `feature-${features.value.length + 1}`
  saving.value = true
  createFeature(props.selected.id, { name, sort_order: features.value.length })
    .then((row) => {
      openFeatureId.value = row.id
      return loadFeatures(props.selected.id)
    })
    .catch((err) =>
      toast.add({ color: 'error', title: 'Error', description: errMsg(err, 'Add feature failed.') }),
    )
    .finally(() => {
      saving.value = false
    })
}

function saveFeature(id) {
  const draft = featureDrafts.value[id]
  if (!draft) return
  saving.value = true
  updateFeature(id, {
    name: draft.name,
    sort_order: draft.sort_order ?? 0,
    add_commands: draft.add_commands ?? '',
    update_commands: draft.update_commands ?? '',
    remove_commands: draft.remove_commands ?? '',
    remove_at_root: !!draft.remove_at_root,
  })
    .then(() => {
      toast.add({ color: 'success', title: 'Successful', description: 'Feature saved' })
      return loadFeatures(props.selected.id)
    })
    .catch((err) =>
      toast.add({ color: 'error', title: 'Error', description: errMsg(err, 'Save failed.') }),
    )
    .finally(() => {
      saving.value = false
    })
}

function removeFeature(id) {
  saving.value = true
  deleteFeature(id)
    .then(() => loadFeatures(props.selected.id))
    .catch((err) =>
      toast.add({ color: 'error', title: 'Error', description: errMsg(err, 'Delete failed.') }),
    )
    .finally(() => {
      saving.value = false
    })
}

function toggleFeature(id) {
  openFeatureId.value = openFeatureId.value === id ? null : id
}
</script>

<template>
  <div class="flex min-h-0 flex-col gap-3 overflow-auto">
    <div v-if="!selected" class="text-muted-color text-sm">Select a scope node.</div>
    <template v-else>
      <h5 class="m-0">{{ selected.title }}</h5>
      <div class="text-sm text-muted-color">
        Kind: {{ selected.kind }}
        <span v-if="selected.device_id"> · device #{{ selected.device_id }}</span>
        <span v-if="selected.interface_id"> · interface #{{ selected.interface_id }}</span>
      </div>

      <template v-if="isCLI">
        <div class="grid grid-cols-1 gap-3 sm:grid-cols-2">
          <div>
            <label class="mb-1 block font-bold">Platform</label>
            <USelectMenu
              v-model="cliForm.platform"
              :items="platformOptions"
              value-key="value"
              label-key="label"
              class="w-full"
            />
          </div>
          <div>
            <label class="mb-1 block font-bold">Payload kind</label>
            <USelectMenu
              v-model="cliForm.payload_kind"
              :items="payloadKindOptions"
              value-key="value"
              label-key="label"
              class="w-full"
            />
          </div>
          <div>
            <label class="mb-1 block font-bold">Service type</label>
            <USelectMenu
              v-model="cliForm.service_type_id"
              :items="typeOptions"
              value-key="value"
              label-key="label"
              class="w-full"
            />
          </div>
          <div class="flex items-end">
            <label class="flex items-center gap-2"
              ><USwitch v-model="cliForm.enabled" /> Enabled</label
            >
          </div>
        </div>
        <div>
          <label class="mb-1 block font-bold">Description</label>
          <UInput v-model="cliForm.description" class="w-full" />
        </div>
        <div>
          <label class="mb-1 block font-bold">Context pattern</label>
          <UInput
            v-model="cliForm.pattern"
            placeholder="interface &lt;name&gt; (empty = global)"
            class="w-full"
          />
        </div>
        <div>
          <label class="mb-1 block font-bold">Enter</label>
          <UInput v-model="cliForm.enter" :placeholder="enterPlaceholder" class="w-full" />
        </div>
        <div>
          <label class="mb-1 block font-bold">Exit</label>
          <UInput v-model="cliForm.exit" placeholder="exit" class="w-full" />
        </div>
        <div v-if="canWrite" class="flex justify-end">
          <UButton label="Save CLI object" :loading="saving" @click="saveCLI" />
        </div>

        <div class="flex items-center justify-between">
          <h6 class="m-0">Features</h6>
          <UButton
            v-if="canWrite"
            size="sm"
            icon="i-lucide-plus"
            label="Add feature"
            @click="addFeature"
          />
        </div>
        <p class="text-muted-color text-sm m-0">
          Each blob is one Go text/template. Update commands are stored but hidden.
        </p>
        <div
          v-for="feat in features"
          :key="feat.id"
          class="border border-default rounded p-3 flex flex-col gap-2"
        >
          <div class="flex flex-wrap items-center gap-2">
            <UButton
              :icon="openFeatureId === feat.id ? 'i-lucide-chevron-down' : 'i-lucide-chevron-right'"
              variant="ghost"
              color="neutral"
              size="sm"
              @click="toggleFeature(feat.id)"
            />
            <UInput
              v-if="featureDrafts[feat.id]"
              v-model="featureDrafts[feat.id].name"
              class="w-40"
              :disabled="!canWrite"
            />
            <div class="ml-auto flex gap-1">
              <UButton
                v-if="canWrite"
                size="sm"
                variant="outline"
                color="neutral"
                label="Save"
                :loading="saving"
                @click="saveFeature(feat.id)"
              />
              <UButton
                v-if="canWrite"
                icon="i-lucide-trash-2"
                variant="outline"
                color="error"
                size="sm"
                @click="removeFeature(feat.id)"
              />
            </div>
          </div>
          <template v-if="openFeatureId === feat.id && featureDrafts[feat.id]">
            <label class="flex items-center gap-2"
              ><USwitch v-model="featureDrafts[feat.id].remove_at_root" :disabled="!canWrite" />
              Remove at root</label
            >
            <div>
              <label class="mb-1 block font-bold">Add commands</label>
              <GoTemplateEditor
                v-model="featureDrafts[feat.id].add_commands"
                class="min-h-48"
                :schema="cliSchema"
                placeholder="Go text/template. One CLI command per output line."
              />
            </div>
            <div>
              <label class="mb-1 block font-bold">Remove commands</label>
              <GoTemplateEditor
                v-model="featureDrafts[feat.id].remove_commands"
                class="min-h-48"
                :schema="cliSchema"
                placeholder="Go text/template. Idempotent teardown."
              />
            </div>
          </template>
        </div>
        <div v-if="!features.length" class="text-muted-color text-sm">No features.</div>
      </template>

      <template v-if="showAssignments">
        <div class="flex items-center justify-between">
          <h6 class="m-0">Assignments</h6>
          <UButton
            v-if="canWrite"
            size="sm"
            icon="i-lucide-plus"
            label="Assign"
            @click="emit('assign')"
          />
        </div>
        <UTable
          :data="assignments"
          :columns="[
            { accessorKey: 'variable_def_id', header: 'Variable' },
            { accessorKey: 'value', header: 'Value' },
            { id: 'actions', header: '' },
          ]"
          empty="No assignments on this node."
        >
          <template #variable_def_id-cell="{ row }">
            {{ assignmentName(row.original.variable_def_id) }}
          </template>
          <template #value-cell="{ row }">
            {{ JSON.stringify(row.original.value) }}
          </template>
          <template #actions-cell="{ row }">
            <div class="flex gap-1">
              <UButton
                icon="i-lucide-pencil"
                variant="outline"
                color="neutral"
                size="sm"
                @click="emit('assign', row.original)"
              />
              <UButton
                v-if="canWrite"
                icon="i-lucide-trash-2"
                variant="outline"
                color="error"
                size="sm"
                @click="emit('delete-assignment', row.original)"
              />
            </div>
          </template>
        </UTable>
        <template v-if="selected.kind === 'interface'">
          <h6 class="m-0">Effective values</h6>
          <UTable
            :data="resolved"
            :columns="[
              { accessorKey: 'name', header: 'Variable' },
              { accessorKey: 'value', header: 'Value' },
              { accessorKey: 'source_name', header: 'Source' },
            ]"
            empty="No variables defined."
          >
            <template #value-cell="{ row }">
              {{ row.original.error || JSON.stringify(row.original.value) }}
            </template>
            <template #source_name-cell="{ row }">
              {{ row.original.from_default ? 'default' : row.original.source_name || '—' }}
            </template>
          </UTable>
        </template>
      </template>
    </template>
  </div>
</template>
