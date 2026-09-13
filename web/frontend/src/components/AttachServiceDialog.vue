<script setup>
import { useToast } from '@nuxt/ui/composables'
import { computed, ref, watch } from 'vue'
import { createScope, listScopes, listServiceTypes } from '@/api/config'
import { getService, getServices, putServiceEndpoints, updateServiceType } from '@/api/services'
import SchemaFields from '@/components/SchemaFields.vue'
import TechnicalServiceForm from '@/components/TechnicalServiceForm.vue'
import {
  endpointsReady,
  findServiceScope,
  findServicesFolderId,
  schemaMissingRequired,
  swallowAttachConflict,
} from '@/utils/serviceEndpoints'

const props = defineProps({
  deviceId: { type: Number, default: null },
  deviceName: { type: String, default: '' },
  interfaceId: { type: Number, default: null },
  interfaceName: { type: String, default: '' },
})

const open = defineModel('open', { type: Boolean, default: false })
const emit = defineEmits(['attached'])

const toast = useToast()
const saving = ref(false)
const submitted = ref(false)

const mode = ref('existing')
const modeItems = [
  { label: 'Existing service', value: 'existing' },
  { label: 'New technical service', value: 'new' },
]

const serviceTypes = ref([])
const services = ref([])
const selectedServiceId = ref(null)
const selectedTypeName = ref(null)
const category = ref('CN')
const schemaValues = ref({})
const connectionTypeId = ref(null)
const endpoints = ref([])
const roleFields = ref({})
const existingEndpoints = ref([])
const createdNode = ref(null)

const categoryOptions = [
  { label: 'CN — External customer', value: 'CN' },
  { label: 'CI — Internal use', value: 'CI' },
]

const typeOptions = computed(() =>
  serviceTypes.value.map((t) => ({
    label: t.description ? `${t.name} — ${t.description}` : t.name,
    value: t.name,
  })),
)
const serviceOptions = computed(() =>
  services.value
    .filter((s) => s.service_type)
    .map((s) => ({
      label: `${s.service_id}${s.service_type ? ` (${s.service_type})` : ''}`,
      value: s.id,
    })),
)

const selectedType = computed(() => {
  if (mode.value === 'existing') {
    const svc = services.value.find((s) => s.id === selectedServiceId.value)
    return serviceTypes.value.find((t) => t.name === svc?.service_type)
  }
  return serviceTypes.value.find((t) => t.name === selectedTypeName.value)
})

const ifaceFields = computed(() => selectedType.value?.interfaces?.fields ?? [])
const atMax = computed(() => {
  const max = selectedType.value?.interfaces?.max ?? 0
  if (max <= 0) return false
  return existingEndpoints.value.length >= max
})

const hydrating = ref(false)

watch(open, (isOpen) => {
  if (!isOpen) return
  submitted.value = false
  mode.value = 'existing'
  selectedServiceId.value = null
  selectedTypeName.value = serviceTypes.value[0]?.name ?? null
  category.value = 'CN'
  schemaValues.value = {}
  connectionTypeId.value = null
  endpoints.value = []
  roleFields.value = {}
  existingEndpoints.value = []
  createdNode.value = null
  hydrating.value = true
  Promise.all([
    listServiceTypes()
      .then((rows) => {
        serviceTypes.value = rows ?? []
        if (!selectedTypeName.value) {
          selectedTypeName.value = serviceTypes.value[0]?.name ?? null
        }
      })
      .catch(() => {}),
    getServices()
      .then((rows) => {
        services.value = rows ?? []
      })
      .catch(() => {}),
  ]).finally(() => {
    hydrating.value = false
  })
})

watch(selectedServiceId, (id) => {
  existingEndpoints.value = []
  if (!id) return
  getService(id)
    .then((data) => {
      existingEndpoints.value = data.endpoints ?? []
    })
    .catch(() => {})
})

watch(selectedTypeName, () => {
  schemaValues.value = {}
  connectionTypeId.value = null
  if (mode.value === 'new') {
    existingEndpoints.value = []
    seedNewEndpoints()
  }
})

watch(mode, () => {
  existingEndpoints.value = []
  roleFields.value = {}
  createdNode.value = null
  if (mode.value === 'new') seedNewEndpoints()
})

function seedNewEndpoints() {
  const st = selectedType.value
  const n = st?.interfaces?.min || 0
  const list = []
  for (let i = 0; i < n; i++) {
    list.push({
      role: 'interface',
      device_id: i === 0 ? props.deviceId : null,
      interface_id: i === 0 ? props.interfaceId : null,
      fields: {},
      label: i === 0 ? `${props.deviceName} / ${props.interfaceName}` : '',
    })
  }
  if (!list.length) {
    list.push({
      role: 'interface',
      device_id: props.deviceId,
      interface_id: props.interfaceId,
      fields: {},
      label: `${props.deviceName} / ${props.interfaceName}`,
    })
  } else {
    list[0] = {
      ...list[0],
      device_id: props.deviceId,
      interface_id: props.interfaceId,
      label: `${props.deviceName} / ${props.interfaceName}`,
    }
  }
  endpoints.value = list
}

function roleFieldsMissing() {
  return schemaMissingRequired(ifaceFields.value, roleFields.value)
}

function endpointsBodyFor(serviceIdMode) {
  if (serviceIdMode === 'new') {
    return endpoints.value.map((ep) => ({
      role: 'interface',
      device_id: ep.device_id,
      interface_id: ep.interface_id,
      fields: ep.fields || {},
    }))
  }
  return [
    ...existingEndpoints.value.map((ep) => ({
      role: 'interface',
      device_id: ep.device_id,
      interface_id: ep.interface_id,
      fields: ep.fields || {},
    })),
    {
      role: 'interface',
      device_id: props.deviceId,
      interface_id: props.interfaceId,
      fields: { ...roleFields.value },
    },
  ]
}

async function treeParentId() {
  const rows = await listScopes()
  const deviceNode = (rows ?? []).find((s) => s.kind === 'device' && s.device_id === props.deviceId)
  if (deviceNode?.parent_id) {
    const p = (rows ?? []).find((s) => s.id === deviceNode.parent_id)
    if (p && (p.kind === 'folder' || p.kind === 'site' || p.kind === 'location')) return p.id
  }
  return findServicesFolderId(async () => rows)
}

function attachExistingPk(parentId, pk) {
  return createScope({
    parent_id: parentId,
    kind: 'service',
    service_id: pk,
  }).catch((err) => swallowAttachConflict(err, findServiceScope(listScopes, pk)))
}

function submit() {
  submitted.value = true
  if (!props.deviceId || !props.interfaceId) return
  if (mode.value === 'existing') {
    if (!selectedServiceId.value || atMax.value) return
    if (roleFieldsMissing()) return
  }
  if (mode.value === 'new') {
    const def = selectedType.value
    if (!def?.name) return
    if ((def.connection_types ?? []).length && !connectionTypeId.value) return
    if (schemaMissingRequired(def.schema, schemaValues.value)) return
    if (!endpointsReady(def.interfaces, endpoints.value)) return
  }

  saving.value = true
  const done = (svc) => {
    toast.add({
      color: 'success',
      title: 'Service attached',
      description: `${svc.service_id || 'Service'} on ${props.interfaceName}`,
      duration: 3000,
    })
    emit('attached', svc)
    open.value = false
  }

  const fail = (err) => {
    toast.add({
      color: 'error',
      title: 'Attach failed',
      description: err?.response?.data?.error ?? 'Failed to attach service.',
      duration: 4000,
    })
  }

  if (mode.value === 'existing') {
    const pk = selectedServiceId.value
    treeParentId()
      .then((parentId) => attachExistingPk(parentId, pk))
      .then(() => putServiceEndpoints(pk, { endpoints: endpointsBodyFor('existing') }))
      .then(() => getService(pk))
      .then(done)
      .catch(fail)
      .finally(() => {
        saving.value = false
      })
    return
  }

  const def = selectedType.value
  const fields = { ...schemaValues.value }
  const realizeId = Number(fields.service_id) || 0
  const putEps = (pk) => putServiceEndpoints(pk, { endpoints: endpointsBodyFor('new') }).then(() => getService(pk))

  if (realizeId > 0) {
    treeParentId()
      .then((parentId) =>
        updateServiceType(realizeId, {
          service_type: def.name,
          fields,
          connection_type_id: connectionTypeId.value || null,
          bandwidth_mbps: Number(fields.bandwidth_mbps) || 0,
          max_mac_addresses: Number(fields.max_mac_addresses) || 0,
        }).then(() => attachExistingPk(parentId, realizeId)),
      )
      .then(() => putEps(realizeId))
      .then(done)
      .catch(fail)
      .finally(() => {
        saving.value = false
      })
    return
  }

  const continueCreated = (node) => {
    createdNode.value = node
    return putEps(node.service_id)
  }

  if (createdNode.value?.service_id) {
    continueCreated(createdNode.value)
      .then(done)
      .catch(fail)
      .finally(() => {
        saving.value = false
      })
    return
  }

  treeParentId()
    .then((parentId) =>
      createScope({
        parent_id: parentId,
        kind: 'service',
        attach: {
          category: category.value,
          service_type: def.name,
          fields,
          connection_type_id: connectionTypeId.value || null,
        },
      }),
    )
    .then(continueCreated)
    .then(done)
    .catch(fail)
    .finally(() => {
      saving.value = false
    })
}
</script>

<template>
  <FormModal
    v-model:open="open"
    :source="{
      mode,
      selectedServiceId,
      selectedTypeName,
      category,
      schemaValues,
      roleName,
      roleFields,
    }"
    :loading="hydrating"
    title="Add service to interface"
    :ui="{ content: 'sm:max-w-lg' }"
    @update:open="(v) => (open = v)"
  >
    <template #body>
      <div class="flex flex-col gap-4">
        <p class="text-sm text-muted-color m-0">{{ deviceName }} / {{ interfaceName }}</p>
        <URadioGroup v-model="mode" :items="modeItems" />

        <template v-if="mode === 'existing'">
          <div>
            <label class="block font-bold mb-2">Service</label>
            <USelectMenu
              v-model="selectedServiceId"
              :items="serviceOptions"
              value-key="value"
              label-key="label"
              placeholder="Select a service"
              class="w-full"
            />
            <small v-if="submitted && !selectedServiceId" class="text-red-500">
              Select a service.
            </small>
            <small v-if="atMax" class="text-red-500">This service already has the maximum interfaces.</small>
          </div>
          <SchemaFields
            v-if="ifaceFields.length"
            v-model="roleFields"
            :fields="ifaceFields"
            :submitted="submitted"
            :interface-id="interfaceId"
            :device-id="deviceId"
          />
        </template>

        <template v-else>
          <div>
            <label class="block font-bold mb-2">Definition</label>
            <USelectMenu
              v-model="selectedTypeName"
              :items="typeOptions"
              value-key="value"
              label-key="label"
              class="w-full"
            />
          </div>
          <div>
            <label class="block font-bold mb-2">Category</label>
            <USelectMenu
              v-model="category"
              :items="categoryOptions"
              value-key="value"
              label-key="label"
              class="w-full"
            />
          </div>
          <TechnicalServiceForm
            v-if="selectedType"
            v-model:fields="schemaValues"
            v-model:connection-type-id="connectionTypeId"
            v-model:endpoints="endpoints"
            :definition="selectedType"
            :submitted="submitted"
          />
        </template>
      </div>
    </template>
    <template #footer>
      <UButton label="Cancel" variant="ghost" @click="open = false" />
      <UButton label="Attach" icon="i-lucide-link" :loading="saving" @click="submit" />
    </template>
  </FormModal>
</template>
