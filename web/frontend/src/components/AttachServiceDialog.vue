<script setup>
import { useToast } from '@nuxt/ui/composables'
import { computed, ref, watch } from 'vue'
import { listServiceTypes } from '@/api/config'
import { createService, getService, getServices, putServiceEndpoints } from '@/api/services'
import SchemaFields from '@/components/SchemaFields.vue'

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
  { label: 'New service', value: 'new' },
]

const serviceTypes = ref([])
const services = ref([])
const selectedServiceId = ref(null)
const selectedTypeName = ref(null)
const category = ref('CN')
const schemaValues = ref({})
const roleName = ref(null)
const roleFields = ref({})
const existingEndpoints = ref([])

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

const availableRoles = computed(() => {
  const st = selectedType.value
  if (!st) return []
  const counts = {}
  for (const ep of existingEndpoints.value) {
    counts[ep.role] = (counts[ep.role] || 0) + 1
  }
  return (st.endpoint_roles ?? []).filter((r) => {
    const n = counts[r.name] || 0
    return r.max === 0 || n < r.max
  })
})
const roleOptions = computed(() =>
  availableRoles.value.map((r) => ({ label: r.name, value: r.name })),
)
const selectedRole = computed(() => availableRoles.value.find((r) => r.name === roleName.value))
const roleFieldDefs = computed(() => selectedRole.value?.fields ?? [])

watch(open, (isOpen) => {
  if (!isOpen) return
  submitted.value = false
  mode.value = 'existing'
  selectedServiceId.value = null
  selectedTypeName.value = serviceTypes.value[0]?.name ?? null
  category.value = 'CN'
  schemaValues.value = {}
  roleName.value = null
  roleFields.value = {}
  existingEndpoints.value = []
  listServiceTypes()
    .then((rows) => {
      serviceTypes.value = rows ?? []
      if (!selectedTypeName.value) {
        selectedTypeName.value = serviceTypes.value[0]?.name ?? null
      }
    })
    .catch(() => {})
  getServices()
    .then((rows) => {
      services.value = rows ?? []
    })
    .catch(() => {})
})

watch(selectedServiceId, (id) => {
  existingEndpoints.value = []
  roleName.value = null
  if (!id) return
  getService(id)
    .then((data) => {
      existingEndpoints.value = data.endpoints ?? []
      pickDefaultRole()
    })
    .catch(() => {})
})

watch(selectedTypeName, () => {
  schemaValues.value = {}
  if (mode.value === 'new') {
    existingEndpoints.value = []
    pickDefaultRole()
  }
})

watch(mode, () => {
  existingEndpoints.value = []
  roleName.value = null
  roleFields.value = {}
  if (mode.value === 'new') pickDefaultRole()
})

watch(roleName, () => {
  roleFields.value = {}
})

function pickDefaultRole() {
  roleName.value = availableRoles.value[0]?.name ?? null
}

function roleFieldsMissing() {
  return roleFieldDefs.value.some((f) => {
    if (!f.required) return false
    const v = roleFields.value[f.name]
    return v === null || v === undefined || v === ''
  })
}

function schemaMissing() {
  return (selectedType.value?.schema ?? []).some((f) => {
    if (!f.required) return false
    const v = schemaValues.value[f.name]
    return v === null || v === undefined || v === ''
  })
}

function attachPayload(serviceId) {
  const endpoints = [
    ...existingEndpoints.value.map((ep) => ({
      role: ep.role,
      device_id: ep.device_id,
      interface_id: ep.interface_id,
      fields: ep.fields || {},
    })),
    {
      role: roleName.value,
      device_id: props.deviceId,
      interface_id: props.interfaceId,
      fields: { ...roleFields.value },
    },
  ]
  return putServiceEndpoints(serviceId, { endpoints })
}

function submit() {
  submitted.value = true
  if (!roleName.value || !props.deviceId || !props.interfaceId) return
  if (roleFieldsMissing()) return
  if (mode.value === 'existing' && !selectedServiceId.value) return
  if (mode.value === 'new') {
    if (!selectedTypeName.value || schemaMissing()) return
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
    attachPayload(selectedServiceId.value)
      .then(() => getService(selectedServiceId.value))
      .then(done)
      .catch(fail)
      .finally(() => {
        saving.value = false
      })
    return
  }

  const fields = { ...schemaValues.value }
  createService({
    category: category.value,
    service_type: selectedTypeName.value,
    bandwidth_mbps: Number(fields.bandwidth_mbps) || 0,
    fields,
    max_mac_addresses: Number(fields.max_mac_addresses) || 0,
  })
    .then((created) => attachPayload(created.id).then(() => created))
    .then(done)
    .catch(fail)
    .finally(() => {
      saving.value = false
    })
}
</script>

<template>
  <UModal
    v-model:open="open"
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
          </div>
        </template>

        <template v-else>
          <div>
            <label class="block font-bold mb-2">Service type</label>
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
          <SchemaFields
            v-if="selectedType?.schema?.length"
            v-model="schemaValues"
            :fields="selectedType.schema"
            :submitted="submitted"
          />
        </template>

        <div>
          <label class="block font-bold mb-2">Role</label>
          <USelectMenu
            v-model="roleName"
            :items="roleOptions"
            value-key="value"
            label-key="label"
            placeholder="No free roles"
            class="w-full"
          />
          <small v-if="submitted && !roleName" class="text-red-500">Select a role.</small>
        </div>
        <SchemaFields
          v-if="roleFieldDefs.length"
          v-model="roleFields"
          :fields="roleFieldDefs"
          :submitted="submitted"
        />
      </div>
    </template>
    <template #footer>
      <UButton label="Cancel" variant="ghost" @click="open = false" />
      <UButton label="Attach" icon="i-lucide-link" :loading="saving" @click="submit" />
    </template>
  </UModal>
</template>
