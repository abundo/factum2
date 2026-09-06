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
import { getDevice } from '@/api/devices'
import { getService, putServiceEndpoints, updateServiceType } from '@/api/services'
import DeviceInterfacePicker from '@/components/DeviceInterfacePicker.vue'
import GoTemplateEditor from '@/components/GoTemplateEditor.vue'
import SchemaFields from '@/components/SchemaFields.vue'
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
  draftEndpoint: { type: Object, default: null },
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
const isParameter = computed(() => props.selected?.kind === 'parameter')
const isServiceNode = computed(
  () => props.selected?.kind === 'service' || props.selected?.kind === 'service_endpoint',
)
const showAssignments = computed(() => {
  const kind = props.selected?.kind
  return (
    kind === 'parameter' ||
    kind === 'folder' ||
    kind === 'site' ||
    kind === 'location' ||
    kind === 'device' ||
    kind === 'interface'
  )
})

const serviceRow = ref(null)
const serviceLoading = ref(false)
const schemaValues = ref({})
const genericEndpoints = ref([])
const genericSaving = ref(false)
const pickerOpen = ref(false)
const genericPickerIndex = ref(null)

const serviceTypeOptions = computed(() => [
  { label: 'Not set', value: '' },
  ...props.serviceTypes.map((t) => ({
    label: t.description ? `${t.name} — ${t.description}` : t.name,
    value: t.name,
  })),
])
const selectedServiceType = computed(() =>
  props.serviceTypes.find((t) => t.name === serviceRow.value?.service_type),
)
const schemaFields = computed(() => selectedServiceType.value?.schema ?? [])
const genericRoles = computed(() => selectedServiceType.value?.endpoint_roles ?? [])
const limeOwned = computed(() => serviceRow.value?.source === 'lime')
const serviceRowId = computed(
  () => props.selected?.service_id || props.selected?.service_row_id || null,
)

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

function loadEndpointLabel(deviceId, interfaceId) {
  if (!deviceId || !interfaceId) return Promise.resolve('')
  return getDevice(deviceId)
    .then((data) => {
      const iface = (data.interfaces ?? []).find((i) => i.id === interfaceId)
      return `${data.name} / ${iface?.name ?? '?'}`
    })
    .catch(() => '')
}

function addGenericEndpoint(roleName, extra = {}) {
  genericEndpoints.value.push({
    role: roleName,
    device_id: extra.device_id ?? null,
    interface_id: extra.interface_id ?? null,
    fields: { ...(extra.fields ?? {}) },
    label: extra.label ?? '',
  })
}

function seedEndpointsForType(typeName) {
  const st = props.serviceTypes.find((x) => x.name === typeName)
  for (const role of st?.endpoint_roles ?? []) {
    const n = role.min || 0
    for (let i = 0; i < n; i++) {
      addGenericEndpoint(role.name)
    }
  }
}

function loadService(id) {
  if (!id) {
    serviceRow.value = null
    genericEndpoints.value = []
    return
  }
  serviceLoading.value = true
  getService(id)
    .then((data) => {
      serviceRow.value = { ...data }
      schemaValues.value = { ...(data.fields ?? {}) }
      if (schemaValues.value.bandwidth_mbps == null && data.bandwidth_mbps) {
        schemaValues.value.bandwidth_mbps = data.bandwidth_mbps
      }
      if (schemaValues.value.max_mac_addresses == null && data.max_mac_addresses) {
        schemaValues.value.max_mac_addresses = data.max_mac_addresses
      }
      genericEndpoints.value = (data.endpoints ?? []).map((ep) => ({
        role: ep.role,
        device_id: ep.device_id,
        interface_id: ep.interface_id,
        fields: { ...(ep.fields ?? {}) },
        label: '',
      }))
      if (genericEndpoints.value.length === 0) {
        const draft = props.draftEndpoint
        if (draft && (!draft.service_id || draft.service_id === id)) {
          addGenericEndpoint(draft.role, draft)
        } else {
          seedEndpointsForType(data.service_type)
        }
      }
      genericEndpoints.value.forEach((ep, i) => {
        loadEndpointLabel(ep.device_id, ep.interface_id).then((label) => {
          if (genericEndpoints.value[i]) genericEndpoints.value[i].label = label
        })
      })
    })
    .catch((err) => {
      serviceRow.value = null
      toast.add({
        color: 'error',
        title: 'Error',
        description: errMsg(err, 'Failed to load service.'),
      })
    })
    .finally(() => {
      serviceLoading.value = false
    })
}

function saveServiceTypeFields() {
  if (!serviceRow.value?.id) return
  saving.value = true
  updateServiceType(serviceRow.value.id, {
    service_type: serviceRow.value.service_type ?? '',
    bandwidth_mbps: Number(schemaValues.value.bandwidth_mbps) || 0,
    max_mac_addresses: Number(schemaValues.value.max_mac_addresses) || 0,
    fields: { ...schemaValues.value },
  })
    .then((data) => {
      serviceRow.value = { ...serviceRow.value, ...data }
      toast.add({ color: 'success', title: 'Successful', description: 'Service type saved' })
      emit('saved')
    })
    .catch((err) =>
      toast.add({
        color: 'error',
        title: 'Error',
        description: errMsg(err, 'Failed to save service type.'),
      }),
    )
    .finally(() => {
      saving.value = false
    })
}

function saveServiceEndpoints() {
  if (!serviceRow.value?.id) return
  genericSaving.value = true
  putServiceEndpoints(serviceRow.value.id, {
    endpoints: genericEndpoints.value.map((ep) => ({
      role: ep.role,
      device_id: ep.device_id,
      interface_id: ep.interface_id,
      fields: ep.fields || {},
    })),
  })
    .then(() => {
      toast.add({ color: 'success', title: 'Successful', description: 'Endpoints saved' })
      emit('saved')
      return loadService(serviceRow.value.id)
    })
    .catch((err) =>
      toast.add({
        color: 'error',
        title: 'Error',
        description: errMsg(err, 'Failed to save endpoints.'),
      }),
    )
    .finally(() => {
      genericSaving.value = false
    })
}

function openGenericPicker(i) {
  genericPickerIndex.value = i
  pickerOpen.value = true
}

function onPickerSelect({ deviceId, deviceName, interfaceId, interfaceName }) {
  const ep = genericEndpoints.value[genericPickerIndex.value]
  if (!ep) return
  ep.device_id = deviceId
  ep.interface_id = interfaceId
  ep.label = `${deviceName} / ${interfaceName}`
}

const pickerDeviceId = computed(
  () => genericEndpoints.value[genericPickerIndex.value]?.device_id ?? null,
)
const pickerInterfaceId = computed(
  () => genericEndpoints.value[genericPickerIndex.value]?.interface_id ?? null,
)

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
    if (isServiceNode.value) {
      loadService(serviceRowId.value)
    } else {
      serviceRow.value = null
      genericEndpoints.value = []
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

      <template v-if="isServiceNode">
        <div v-if="serviceLoading" class="text-muted-color text-sm">Loading service…</div>
        <template v-else-if="serviceRow">
          <div class="text-sm">
            Service ID:
            <RouterLink to="/service" class="underline">{{ serviceRow.service_id }}</RouterLink>
            <span v-if="limeOwned" class="text-muted-color"> · Lime (commercial fields read-only)</span>
          </div>
          <div>
            <label class="mb-1 block font-bold">Type</label>
            <USelectMenu
              v-model="serviceRow.service_type"
              :items="serviceTypeOptions"
              value-key="value"
              label-key="label"
              :disabled="!canWrite"
              class="w-full"
            />
          </div>
          <SchemaFields
            v-if="schemaFields.length"
            v-model="schemaValues"
            :fields="schemaFields"
            :disabled="!canWrite"
          />
          <div v-if="canWrite" class="flex justify-end">
            <UButton
              label="Save type"
              :loading="saving"
              @click="saveServiceTypeFields"
            />
          </div>
          <h6 class="m-0">Endpoints</h6>
          <p class="text-muted-color text-sm m-0">
            Unsaved rows are not written until you save a complete set.
          </p>
          <div
            v-for="(ep, i) in genericEndpoints"
            :key="i"
            class="border border-default rounded p-3 flex flex-col gap-2"
          >
            <div>
              <label class="mb-1 block font-bold">Role</label>
              <USelectMenu
                v-model="ep.role"
                :items="genericRoles.map((r) => ({ label: r.name, value: r.name }))"
                value-key="value"
                label-key="label"
                :disabled="!canWrite"
                class="w-full"
              />
            </div>
            <div>
              <label class="mb-1 block font-bold">Device / interface</label>
              <div class="flex items-center gap-2">
                <UInput :model-value="ep.label" disabled placeholder="Not selected" class="w-full" />
                <UButton
                  v-if="canWrite"
                  icon="i-lucide-list-tree"
                  variant="outline"
                  color="neutral"
                  @click="openGenericPicker(i)"
                />
              </div>
            </div>
            <template
              v-for="field in genericRoles.find((r) => r.name === ep.role)?.fields ?? []"
              :key="field.name"
            >
              <label class="mb-1 block font-bold">{{ field.name }}</label>
              <UInput v-model="ep.fields[field.name]" :disabled="!canWrite" />
            </template>
            <div v-if="canWrite" class="flex justify-end">
              <UButton
                label="Remove"
                variant="ghost"
                color="error"
                size="sm"
                @click="genericEndpoints.splice(i, 1)"
              />
            </div>
          </div>
          <div class="flex flex-wrap gap-2">
            <UButton
              v-for="role in genericRoles"
              :key="role.name"
              :label="`Add ${role.name}`"
              variant="outline"
              color="neutral"
              size="sm"
              :disabled="!canWrite"
              @click="addGenericEndpoint(role.name)"
            />
          </div>
          <div v-if="canWrite" class="flex justify-end">
            <UButton
              label="Save endpoints"
              :loading="genericSaving"
              @click="saveServiceEndpoints"
            />
          </div>
        </template>
        <DeviceInterfacePicker
          v-model:open="pickerOpen"
          :device-id="pickerDeviceId"
          :interface-id="pickerInterfaceId"
          @select="onPickerSelect"
        />
      </template>

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
        <p v-if="isParameter" class="text-muted-color text-sm m-0">
          Assignments on this object apply to the parent scope and its descendants.
        </p>
        <p v-else class="text-muted-color text-sm m-0">
          Prefer assigning on a parameter object. Saving here still remaps onto the reserved
          parameters child.
        </p>
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
