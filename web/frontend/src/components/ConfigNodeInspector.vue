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
import {
  getService,
  putServiceEndpoints,
  unrealizeService,
  updateServiceType,
} from '@/api/services'
import GoTemplateEditor from '@/components/GoTemplateEditor.vue'
import ServiceInstanceForm from '@/components/ServiceInstanceForm.vue'
import { reshapeEndpoints } from '@/utils/serviceEndpoints'
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

const emit = defineEmits(['assign', 'delete-assignment', 'saved', 'delete-service'])

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
const isResource = computed(() => props.selected?.kind === 'resource')
const resourceForm = ref({ name: '', description: '', enabled: true, cidrs: [''] })
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
const instanceSubmitted = ref(false)
const unrealizeOpen = ref(false)
const unrealizing = ref(false)
const unrealizeRemoveNetbox = ref(false)
const unrealizeRemoveDevice = ref(false)

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
const connectionTypeId = computed({
  get: () => serviceRow.value?.connection_type_id ?? null,
  set: (v) => {
    if (serviceRow.value) serviceRow.value.connection_type_id = v
  },
})
const limeOwned = computed(() => serviceRow.value?.source === 'lime')
const isRealized = computed(() => Boolean(serviceRow.value?.service_type))
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
  } else if (!openFeatureId.value && features.value.length === 1) {
    openFeatureId.value = features.value[0].id
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
      schemaValues.value = { ...data.fields }
      if (schemaValues.value.bandwidth_mbps == null && data.bandwidth_mbps) {
        schemaValues.value.bandwidth_mbps = data.bandwidth_mbps
      }
      if (schemaValues.value.max_mac_addresses == null && data.max_mac_addresses) {
        schemaValues.value.max_mac_addresses = data.max_mac_addresses
      }
      const mapped = (data.endpoints ?? []).map((ep) => ({
        role: 'interface',
        device_id: ep.device_id,
        interface_id: ep.interface_id,
        fields: { ...ep.fields },
        label: '',
      }))
      const st = props.serviceTypes.find((x) => x.name === data.service_type)
      const draft = props.draftEndpoint
      genericEndpoints.value = reshapeEndpoints(
        st?.interfaces,
        mapped,
        draft && (!draft.service_id || draft.service_id === id) ? draft : null,
      )
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
  instanceSubmitted.value = true
  updateServiceType(serviceRow.value.id, {
    service_type: serviceRow.value.service_type ?? '',
    bandwidth_mbps: Number(schemaValues.value.bandwidth_mbps) || 0,
    max_mac_addresses: Number(schemaValues.value.max_mac_addresses) || 0,
    fields: { ...schemaValues.value },
    connection_type_id: serviceRow.value.connection_type_id || null,
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
      role: 'interface',
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
    .catch((err) => {
      toast.add({
        color: 'error',
        title: 'Error',
        description: errMsg(err, 'Failed to save endpoints.'),
      })
    })
    .finally(() => {
      genericSaving.value = false
    })
}

function openUnrealize() {
  unrealizeRemoveNetbox.value = Boolean(serviceRow.value?.l2vpn_netbox_id)
  unrealizeRemoveDevice.value = Boolean(serviceRow.value?.applied_to_device)
  unrealizeOpen.value = true
}

function doUnrealize() {
  if (!serviceRow.value?.id) return
  unrealizing.value = true
  unrealizeService(serviceRow.value.id, {
    remove_from_netbox: unrealizeRemoveNetbox.value,
    remove_from_device: unrealizeRemoveDevice.value,
  })
    .then((data) => {
      unrealizeOpen.value = false
      toast.add({
        color: 'success',
        title: 'Unrealized',
        description: 'Technical realization removed.',
      })
      if (data?.service) {
        serviceRow.value = { ...serviceRow.value, ...data.service }
        schemaValues.value = { ...data.service.fields }
        genericEndpoints.value = []
      }
      emit('saved')
    })
    .catch((err) => {
      toast.add({
        color: 'error',
        title: 'Error',
        description: errMsg(err, 'Failed to unrealize.'),
      })
    })
    .finally(() => {
      unrealizing.value = false
    })
}

function confirmUnrealize() {
  doUnrealize()
}

function requestDeleteService() {
  if (!serviceRow.value?.id) return
  emit('delete-service', {
    id: props.selected?.id,
    service_id: serviceRow.value.id,
    title: serviceRow.value.service_id || props.selected?.title,
  })
}

function resetResourceForm(node) {
  const cidrs = node?.payload?.cidrs
  resourceForm.value = {
    name: node?.title ?? '',
    description: node?.payload?.description ?? '',
    enabled: node?.enabled !== false,
    cidrs: Array.isArray(cidrs) && cidrs.length ? [...cidrs] : [''],
  }
}

function addResourceCIDR() {
  resourceForm.value.cidrs = [...resourceForm.value.cidrs, '']
}

function removeResourceCIDR(i) {
  const next = resourceForm.value.cidrs.filter((_, idx) => idx !== i)
  resourceForm.value.cidrs = next.length ? next : ['']
}

function saveResource() {
  if (!props.selected?.id) return
  const name = (resourceForm.value.name ?? '').trim()
  if (!name) {
    toast.add({ color: 'error', title: 'Error', description: 'Name is required.' })
    return
  }
  saving.value = true
  const prev = props.selected?.payload ?? {}
  const cidrs = (resourceForm.value.cidrs ?? []).map((s) => (s ?? '').trim()).filter(Boolean)
  updateScope(props.selected.id, {
    name,
    enabled: !!resourceForm.value.enabled,
    payload: { ...prev, description: resourceForm.value.description ?? '', cidrs },
  })
    .then(() => {
      toast.add({ color: 'success', title: 'Successful', description: 'Resource saved' })
      emit('saved')
    })
    .catch((err) =>
      toast.add({ color: 'error', title: 'Error', description: errMsg(err, 'Save failed.') }),
    )
    .finally(() => {
      saving.value = false
    })
}

watch(
  () => [props.selected?.id, props.selected?.kind],
  ([id, kind]) => {
    if (kind === 'cli' && id) {
      resetCLIForm(props.selected)
      loadFeatures(id)
    } else {
      features.value = []
      featureDrafts.value = {}
      openFeatureId.value = null
    }
    if (kind === 'resource') {
      resetResourceForm(props.selected)
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

watch(
  () => serviceRow.value?.service_type,
  (t, prev) => {
    if (!t || t === prev || serviceLoading.value) return
    const st = props.serviceTypes.find((x) => x.name === t)
    genericEndpoints.value = reshapeEndpoints(st?.interfaces, genericEndpoints.value)
    genericEndpoints.value.forEach((ep, i) => {
      loadEndpointLabel(ep.device_id, ep.interface_id).then((label) => {
        if (genericEndpoints.value[i] && label) genericEndpoints.value[i].label = label
      })
    })
  },
)

function featurePayload(draft) {
  return {
    name: draft.name,
    sort_order: draft.sort_order ?? 0,
    add_commands: draft.add_commands ?? '',
    update_commands: draft.update_commands ?? '',
    remove_commands: draft.remove_commands ?? '',
    remove_at_root: !!draft.remove_at_root,
  }
}

function patchFeatureDraft(id, field, value) {
  const draft = featureDrafts.value[id]
  if (!draft) return
  draft[field] = value
}

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
      ...prev.context,
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
    .then(() =>
      Promise.all(
        features.value.map((feat) => {
          const draft = featureDrafts.value[feat.id]
          if (!draft) return null
          return updateFeature(feat.id, featurePayload(draft))
        }),
      ),
    )
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
      toast.add({
        color: 'error',
        title: 'Error',
        description: errMsg(err, 'Add feature failed.'),
      }),
    )
    .finally(() => {
      saving.value = false
    })
}

function saveFeature(id) {
  const draft = featureDrafts.value[id]
  if (!draft) return
  saving.value = true
  updateFeature(id, featurePayload(draft))
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
  <div class="flex h-full min-h-0 flex-1 flex-col gap-3 overflow-auto">
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
            <span v-if="limeOwned" class="text-muted-color">
              · Lime (commercial fields read-only)</span
            >
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
          <ServiceInstanceForm
            v-if="selectedServiceType"
            v-model:fields="schemaValues"
            v-model:connection-type-id="connectionTypeId"
            v-model:endpoints="genericEndpoints"
            :definition="selectedServiceType"
            :disabled="!canWrite"
            :submitted="instanceSubmitted"
          />
          <p class="text-muted-color text-sm m-0">
            Unsaved rows are not written until you save a complete set.
          </p>
          <div v-if="canWrite" class="flex flex-wrap justify-end gap-2">
            <UButton
              v-if="isRealized"
              label="Unrealize"
              variant="outline"
              color="error"
              @click="openUnrealize"
            />
            <UButton
              v-if="!limeOwned"
              label="Delete service"
              color="error"
              @click="requestDeleteService"
            />
            <UButton label="Save type" :loading="saving" @click="saveServiceTypeFields" />
            <UButton
              label="Save endpoints"
              :loading="genericSaving"
              @click="saveServiceEndpoints"
            />
          </div>
        </template>
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
          <label class="mb-1 block font-bold">Context pattern (regex)</label>
          <p class="text-muted-color text-sm mb-1 m-0">
            Anchored regex. Empty or <code>global</code> matches the configure root.
            <code>&lt;ident&gt;</code> captures a token, e.g. <code>interface &lt;name&gt;</code>.
          </p>
          <UInput
            v-model="cliForm.pattern"
            placeholder="interface &lt;name&gt; (empty = global)"
            class="w-full font-mono"
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
          Each blob is one Jet template. Update commands are stored but hidden.
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
                :model-value="featureDrafts[feat.id].add_commands"
                compact
                :line-wrapping="false"
                :autofocus="false"
                :schema="cliSchema"
                placeholder="Jet template. One CLI command per output line."
                @update:model-value="(v) => patchFeatureDraft(feat.id, 'add_commands', v)"
                @apply="saveFeature(feat.id)"
              />
            </div>
            <div>
              <label class="mb-1 block font-bold">Remove commands</label>
              <GoTemplateEditor
                :model-value="featureDrafts[feat.id].remove_commands"
                compact
                :line-wrapping="false"
                :autofocus="false"
                :schema="cliSchema"
                placeholder="Jet template. Idempotent teardown."
                @update:model-value="(v) => patchFeatureDraft(feat.id, 'remove_commands', v)"
                @apply="saveFeature(feat.id)"
              />
            </div>
            <div v-if="canWrite" class="flex justify-end">
              <UButton
                label="Save commands"
                :loading="saving"
                type="button"
                @click="saveFeature(feat.id)"
              />
            </div>
          </template>
        </div>
        <div v-if="!features.length" class="text-muted-color text-sm">No features.</div>
      </template>

      <template v-if="isResource">
        <div>
          <label class="mb-1 block font-bold">Name</label>
          <UInput v-model="resourceForm.name" :disabled="!canWrite" class="w-full" />
        </div>
        <div>
          <label class="mb-1 block font-bold">Description</label>
          <UInput v-model="resourceForm.description" :disabled="!canWrite" class="w-full" />
        </div>
        <div class="flex items-end">
          <label class="flex items-center gap-2"
            ><USwitch v-model="resourceForm.enabled" :disabled="!canWrite" /> Enabled</label
          >
        </div>
        <div>
          <div class="mb-1 flex items-center justify-between">
            <label class="block font-bold">CIDRs</label>
            <UButton
              v-if="canWrite"
              size="sm"
              variant="outline"
              color="neutral"
              icon="i-lucide-plus"
              label="Add"
              @click="addResourceCIDR"
            />
          </div>
          <div v-for="(_, i) in resourceForm.cidrs" :key="i" class="mb-2 flex items-center gap-2">
            <UInput
              v-model="resourceForm.cidrs[i]"
              :disabled="!canWrite"
              placeholder="10.0.0.0/31"
              class="w-full font-mono"
            />
            <UButton
              v-if="canWrite"
              icon="i-lucide-trash-2"
              variant="ghost"
              color="error"
              size="sm"
              @click="removeResourceCIDR(i)"
            />
          </div>
        </div>
        <div v-if="canWrite" class="flex justify-end">
          <UButton label="Save" :loading="saving" @click="saveResource" />
        </div>
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

  <UModal v-model:open="unrealizeOpen" title="Unrealize service" :ui="{ content: 'sm:max-w-md' }">
    <template #body>
      <p class="text-sm m-0">
        Drops the technical realization (type, endpoints, tree node) and keeps the commercial row.
      </p>
      <label class="flex items-center gap-2 mt-3">
        <UCheckbox v-model="unrealizeRemoveNetbox" />
        Remove from NetBox
      </label>
      <label class="flex items-center gap-2">
        <UCheckbox v-model="unrealizeRemoveDevice" />
        Remove from device
      </label>
    </template>
    <template #footer>
      <UButton label="Cancel" variant="ghost" @click="unrealizeOpen = false" />
      <UButton label="Unrealize" color="error" :loading="unrealizing" @click="confirmUnrealize" />
    </template>
  </UModal>
</template>
