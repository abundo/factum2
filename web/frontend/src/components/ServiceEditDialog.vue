<script setup>
import { useToast } from '@nuxt/ui/composables'
import { computed, ref, watch } from 'vue'
import { listServiceTypes } from '@/api/config'
import { getCustomers } from '@/api/customers'
import { getDevice } from '@/api/devices'
import {
  deleteService,
  getService,
  pushService,
  putServiceEndpoints,
  updateService,
  updateServiceType,
} from '@/api/services'
import { getServicePath, putServicePath } from '@/api/optical'
import DeviceInterfacePicker from '@/components/DeviceInterfacePicker.vue'
import PasswordInput from '@/components/PasswordInput.vue'
import { useDeviceCredentials } from '@/composables/useDeviceCredentials'
import { useAuthStore } from '@/stores/auth'

const authStore = useAuthStore()
const {
  credentialsDialog,
  promptUsername,
  promptPassword,
  withCredentials,
  submitCredentials,
  cancelCredentials,
  rememberSuccess,
  rememberFailure,
} = useDeviceCredentials()

const props = defineProps({
  serviceId: { type: Number, default: null },
})

const open = defineModel('open', { type: Boolean, default: false })

// Emitted after a save/delete actually succeeds, so a caller with its own
// list of services (ServiceList.vue) or of interfaces (DeviceList.vue) knows
// to reload - this dialog has no list of its own to keep in sync.
const emit = defineEmits(['saved', 'deleted'])

const toast = useToast()

const customers = ref([])
const service = ref({})
const submitted = ref(false)
const saving = ref(false)
const loading = ref(false)

const deleteDialog = ref(false)
const deleteTarget = ref(null)
const deleting = ref(false)
const deleteRemoveNetbox = ref(false)
const deleteRemoveDevice = ref(false)

const pickerOpen = ref(false)
const pickerTarget = ref(null)

// Mirrors ServiceCreateWizard.vue's capacity-product subset (ELINE/ELAN/
// L3VPN/POLARIX) - Wavelength/Fiber have no ServiceType, so they aren't
// offered here.
const FALLBACK_SERVICE_TYPES = [
  { label: 'Not set', value: '' },
  { label: 'ELINE — L2VPN point to point', value: 'ELINE' },
  { label: 'ELAN — L2VPN multipoint', value: 'ELAN' },
  { label: 'L3VPN — L3 multipoint', value: 'L3VPN' },
  { label: 'Internet — Polarix', value: 'POLARIX' },
]
const serviceTypeRows = ref([])
const serviceTypeOptions = computed(() => {
  if (!serviceTypeRows.value.length) return FALLBACK_SERVICE_TYPES
  return [
    { label: 'Not set', value: '' },
    ...serviceTypeRows.value.map((t) => ({
      label: t.description ? `${t.name} — ${t.description}` : t.name,
      value: t.name,
    })),
  ]
})
const selectedServiceType = computed(() =>
  serviceTypeRows.value.find((t) => t.name === service.value.service_type),
)
const genericRoles = computed(() =>
  service.value.service_type ? (selectedServiceType.value?.endpoint_roles ?? []) : [],
)
const genericEndpoints = ref([])
const genericSaving = ref(false)
const genericPushing = ref(false)
const genericPushResults = ref([])
const genericPickerIndex = ref(null)

const customerOptions = computed(() => customers.value.map((c) => ({ id: c.id, name: c.name })))

// Lime-synced services are overwritten wholesale on every sync run, so an
// edit to a Lime-owned field (company/delivery points/product/service/
// comment/agreement status) made here would just silently vanish on the
// next sync - the API rejects PUTs to those fields too (see
// ApiServiceUpdate in web/handler_service.go). Service type/bandwidth/max
// MAC addresses and ELINE provisioning are unaffected by readOnly - see
// saveServiceType and ApiServiceTypeUpdate, which are the one part of a
// Lime row this dialog can still save.
const readOnly = computed(() => service.value.source === 'lime')

// A viewer (or a user with no role) can open this dialog to look, but every
// control that would save/delete/provision anything is disabled - the API
// rejects those calls outright (web/auth.go's RequireWrite), so disabling
// them here just avoids a submit-then-403 round trip.
const canWrite = computed(() => authStore.canWrite)

function loadServiceTypes() {
  if (serviceTypeRows.value.length > 0) return
  listServiceTypes()
    .then((rows) => {
      serviceTypeRows.value = rows ?? []
    })
    .catch(() => {})
}

function loadCustomers() {
  if (customers.value.length > 0) return
  getCustomers()
    .then((data) => {
      customers.value = data ?? []
    })
    .catch(() => {
      // Customer names are only needed for the company select, not critical.
    })
}

// Resolves a stored endpoint (device_id/interface_id) to a display label -
// the service row itself only carries the IDs, so this is only fetched
// when the dialog is opened for an already-provisioned ELINE service.
function loadEndpointLabel(deviceId, interfaceId) {
  if (!deviceId || !interfaceId) return Promise.resolve('')
  return getDevice(deviceId)
    .then((data) => {
      const iface = (data.interfaces ?? []).find((i) => i.id === interfaceId)
      return `${data.name} / ${iface?.name ?? '?'}`
    })
    .catch(() => '')
}

const path = ref(null)
const pathA = ref(null)
const pathZ = ref(null)
const pathPicker = ref(null)
const opticalCat = computed(() => {
  const id = service.value?.service_id ?? ''
  return /^(VL|VI|LF|LI)\d{5}$/.test(id) ? id.slice(0, 2) : ''
})
const pathMode = computed(() =>
  opticalCat.value === 'LF' || opticalCat.value === 'LI' ? 'fiber' : 'wavelength',
)

function loadPath(id) {
  if (!authStore.opticalEnabled || !id) return
  getServicePath(id)
    .then((data) => {
      path.value = data
    })
    .catch(() => {
      path.value = null
    })
}

function onPathPick(sel) {
  if (pathPicker.value === 'a') pathA.value = sel.interfaceId
  if (pathPicker.value === 'z') pathZ.value = sel.interfaceId
  pathPicker.value = null
}

function attachPath() {
  if (!service.value?.id || !pathA.value || !pathZ.value) return
  putServicePath(service.value.id, {
    interface_a_id: pathA.value,
    interface_z_id: pathZ.value,
    mode: pathMode.value === 'fiber' ? 'fiber' : 'wdm',
  })
    .then((data) => {
      path.value = data
    })
    .catch((err) => {
      toast.add({
        color: 'error',
        title: 'Path attach failed',
        description: err?.response?.data?.error,
      })
    })
}

function loadServiceById(id) {
  submitted.value = false
  loading.value = true
  getService(id)
    .then((data) => {
      service.value = { ...data }
      loadPath(data.id)
      genericEndpoints.value = (data.endpoints ?? []).map((ep) => ({
        role: ep.role,
        device_id: ep.device_id,
        interface_id: ep.interface_id,
        fields: { ...ep.fields },
        label: '',
      }))
      if (data.service_type === 'ELINE' && genericEndpoints.value.length === 0) {
        genericEndpoints.value = [
          { role: 'a', device_id: null, interface_id: null, fields: {}, label: '' },
          { role: 'b', device_id: null, interface_id: null, fields: {}, label: '' },
        ]
      }
      genericEndpoints.value.forEach((ep, i) => {
        loadEndpointLabel(ep.device_id, ep.interface_id).then((label) => {
          genericEndpoints.value[i].label = label
        })
      })
    })
    .catch(() => {
      toast.add({
        color: 'error',
        title: 'Error',
        description: 'Failed to load service.',
        duration: 3000,
      })
    })
    .finally(() => {
      loading.value = false
    })
}

watch(open, (isOpen) => {
  if (!isOpen || !props.serviceId) return
  loadCustomers()
  loadServiceTypes()
  loadServiceById(props.serviceId)
})

watch(
  () => service.value.service_type,
  (t) => {
    if (t === 'ELINE' && genericEndpoints.value.length === 0) {
      genericEndpoints.value = [
        { role: 'a', device_id: null, interface_id: null, fields: {}, label: '' },
        { role: 'b', device_id: null, interface_id: null, fields: {}, label: '' },
      ]
    }
  },
)

function hideDialog() {
  open.value = false
  submitted.value = false
}

function onPickerSelect({ deviceId, deviceName, interfaceId, interfaceName }) {
  const label = `${deviceName} / ${interfaceName}`
  if (pickerTarget.value === 'generic' && genericPickerIndex.value != null) {
    const ep = genericEndpoints.value[genericPickerIndex.value]
    if (ep) {
      ep.device_id = deviceId
      ep.interface_id = interfaceId
      ep.label = label
    }
  }
}

function addGenericEndpoint(roleName) {
  genericEndpoints.value.push({
    role: roleName,
    device_id: null,
    interface_id: null,
    fields: {},
    label: '',
  })
}

function removeGenericEndpoint(i) {
  genericEndpoints.value.splice(i, 1)
}

function openGenericPicker(i) {
  genericPickerIndex.value = i
  pickerTarget.value = 'generic'
  pickerOpen.value = true
}

function saveGenericEndpoints() {
  genericSaving.value = true
  const payload = {
    endpoints: genericEndpoints.value.map((ep) => ({
      role: ep.role,
      device_id: ep.device_id,
      interface_id: ep.interface_id,
      fields: ep.fields || {},
    })),
  }
  putServiceEndpoints(service.value.id, payload)
    .then((rows) => {
      service.value.endpoints = rows
      return getService(service.value.id)
    })
    .then((data) => {
      if (data) {
        service.value = { ...service.value, ...data }
      }
      toast.add({
        color: 'success',
        title: 'Successful',
        description: 'Endpoints saved',
        duration: 3000,
      })
      emit('saved')
    })
    .catch((err) => {
      toast.add({
        color: 'error',
        title: 'Error',
        description: err?.response?.data?.error ?? 'Failed to save endpoints.',
        duration: 4000,
      })
    })
    .finally(() => {
      genericSaving.value = false
    })
}

function genericDeviceIds() {
  return [...new Set(genericEndpoints.value.map((ep) => ep.device_id).filter(Boolean))]
}

function saveAndPushGeneric() {
  genericSaving.value = true
  const payload = {
    endpoints: genericEndpoints.value.map((ep) => ({
      role: ep.role,
      device_id: ep.device_id,
      interface_id: ep.interface_id,
      fields: ep.fields || {},
    })),
  }
  putServiceEndpoints(service.value.id, payload)
    .then((rows) => {
      service.value.endpoints = rows
      emit('saved')
      withCredentials(genericDeviceIds(), doPushGeneric)
    })
    .catch((err) => {
      toast.add({
        color: 'error',
        title: 'Error',
        description: err?.response?.data?.error ?? 'Failed to save endpoints.',
        duration: 4000,
      })
    })
    .finally(() => {
      genericSaving.value = false
    })
}

function doPushGeneric(username, password) {
  const deviceIds = genericDeviceIds()
  genericPushing.value = true
  pushService(service.value.id, { username, password })
    .then((data) => {
      genericPushResults.value = data.results ?? []
      rememberSuccess(deviceIds, username, password)
      const failed = genericPushResults.value.filter((r) => r.error)
      if (failed.length === 0) {
        toast.add({
          color: 'success',
          title: 'Pushed to devices',
          description: 'Service config was applied.',
          duration: 3000,
        })
      } else {
        toast.add({
          color: 'error',
          title: 'Push completed with errors',
          description: `${failed.length} of ${genericPushResults.value.length} device(s) failed.`,
          duration: 5000,
        })
      }
    })
    .catch((err) => {
      rememberFailure(deviceIds, username, password)
      toast.add({
        color: 'error',
        title: 'Push failed',
        description: err?.response?.data?.error ?? 'Failed to push config.',
        duration: 4000,
      })
    })
    .finally(() => {
      genericPushing.value = false
    })
}

// A Lime-synced service can only ever have its type/bandwidth/max MAC
// addresses saved here (the rest of the record is Lime-owned and stays
// read-only) - see updateServiceType/ApiServiceTypeUpdate.
function saveServiceType() {
  saving.value = true
  const payload = {
    service_type: service.value.service_type ?? '',
    bandwidth_mbps: Number(service.value.bandwidth_mbps) || 0,
    max_mac_addresses:
      service.value.service_type === 'ELAN' ? Number(service.value.max_mac_addresses) || 0 : 0,
  }

  updateServiceType(service.value.id, payload)
    .then((data) => {
      service.value = { ...service.value, ...data }
      toast.add({
        color: 'success',
        title: 'Successful',
        description: 'Service updated',
        duration: 3000,
      })
      emit('saved')
    })
    .catch(() => {
      toast.add({
        color: 'error',
        title: 'Error',
        description: 'Failed to save service.',
        duration: 3000,
      })
    })
    .finally(() => {
      saving.value = false
    })
}

function saveService() {
  if (readOnly.value) {
    saveServiceType()
    return
  }
  submitted.value = true

  if (!service.value.service_id?.trim() || !service.value.company) {
    return
  }

  saving.value = true
  const payload = { ...service.value }

  updateService(payload.id, payload)
    .then(() => {
      toast.add({
        color: 'success',
        title: 'Successful',
        description: 'Service updated',
        duration: 3000,
      })
      open.value = false
      emit('saved')
    })
    .catch(() => {
      toast.add({
        color: 'error',
        title: 'Error',
        description: 'Failed to save service.',
        duration: 3000,
      })
    })
    .finally(() => {
      saving.value = false
    })
}

function confirmDelete(row) {
  deleteTarget.value = row
  // Default to a full teardown when applicable - deleting a service is
  // assumed to mean it should stop existing everywhere, not just in
  // factum's own DB. row is always the full getService(id) result (see
  // loadServiceById), which includes l2vpn_netbox_id/applied_to_device.
  deleteRemoveNetbox.value = Boolean(row?.l2vpn_netbox_id)
  deleteRemoveDevice.value = Boolean(row?.applied_to_device)
  deleteDialog.value = true
}

function hideDeleteDialog() {
  deleteDialog.value = false
  deleteTarget.value = null
  deleteRemoveNetbox.value = false
  deleteRemoveDevice.value = false
}

function doDeleteService(username, password) {
  if (!deleteTarget.value) return

  const deviceIds = genericDeviceIds()
  deleting.value = true
  deleteService(deleteTarget.value.id, {
    remove_from_netbox: deleteRemoveNetbox.value,
    remove_from_device: deleteRemoveDevice.value,
    username,
    password,
  })
    .then(() => {
      if (username && password) {
        rememberSuccess(deviceIds, username, password)
      }
      toast.add({
        color: 'success',
        title: 'Successful',
        description: 'Service deleted',
        duration: 3000,
      })
      deleteDialog.value = false
      deleteTarget.value = null
      open.value = false
      emit('deleted')
    })
    .catch((err) => {
      if (username && password) {
        rememberFailure(deviceIds, username, password)
      }
      toast.add({
        color: 'error',
        title: 'Error',
        description: err?.response?.data?.error ?? 'Failed to delete service.',
        duration: 5000,
      })
    })
    .finally(() => {
      deleting.value = false
    })
}

// Removing the device config is a real device operation, so it needs
// credentials. The backend blocks the whole delete (row kept) if that
// cleanup fails. Closes the delete dialog first when credentials are
// needed so UModal backdrops don't stack three deep.
function deleteServiceConfirmed() {
  if (!deleteTarget.value) return
  if (deleteRemoveDevice.value) {
    deleteDialog.value = false
    withCredentials(genericDeviceIds(), doDeleteService)
  } else {
    doDeleteService('', '')
  }
}
</script>

<template>
  <UModal
    v-model:open="open"
    :title="readOnly ? 'Service Details (synced from Lime)' : 'Service Details'"
    :ui="{ content: 'sm:max-w-lg' }"
  >
    <template #body>
      <div v-if="loading" class="flex justify-center p-4">
        <UIcon name="i-lucide-loader-2" class="size-8 animate-spin" />
      </div>

      <template v-else>
        <div class="grid grid-cols-[9rem_1fr] items-center gap-y-4 gap-x-3">
          <label for="service_id" class="font-bold">Service ID</label>
          <UInput id="service_id" v-model="service.service_id" disabled class="w-full" />

          <label for="agreement_status" class="font-bold">Agreement status</label>
          <UInput
            id="agreement_status"
            v-model="service.agreement_status"
            disabled
            class="w-full"
          />

          <label for="company" class="font-bold">Company</label>
          <div>
            <USelectMenu
              id="company"
              v-model="service.company"
              :disabled="readOnly || !canWrite"
              :items="customerOptions"
              value-key="id"
              label-key="name"
              placeholder="Select a customer"
              :color="submitted && !service.company ? 'error' : undefined"
              :highlight="submitted && !service.company"
              class="w-full"
            />
            <small v-if="submitted && !service.company" class="text-red-500"
              >Company is required.</small
            >
          </div>

          <label for="deliverypoint1" class="font-bold">Deliverypoint A</label>
          <UInput
            id="deliverypoint1"
            v-model="service.deliverypoint1"
            :disabled="readOnly || !canWrite"
            class="w-full"
          />

          <label for="deliverypoint2" class="font-bold">Deliverypoint B</label>
          <UInput
            id="deliverypoint2"
            v-model="service.deliverypoint2"
            :disabled="readOnly || !canWrite"
            class="w-full"
          />

          <label for="product" class="font-bold">Product</label>
          <UInput
            id="product"
            v-model="service.product"
            :disabled="readOnly || !canWrite"
            class="w-full"
          />

          <label for="serviceField" class="font-bold">Service</label>
          <UInput
            id="serviceField"
            v-model="service.service"
            :disabled="readOnly || !canWrite"
            class="w-full"
          />

          <label for="comment" class="font-bold self-start mt-2">Comment</label>
          <UTextarea
            id="comment"
            v-model="service.comment"
            :disabled="readOnly || !canWrite"
            :rows="3"
            class="w-full"
          />

          <template v-if="service.source">
            <label for="source" class="font-bold">Source</label>
            <UInput id="source" v-model="service.source" disabled class="w-full" />
          </template>
        </div>

        <hr class="my-6" />
        <div class="flex flex-col gap-4">
          <h5 class="m-0">Service type</h5>

          <div class="grid grid-cols-[9rem_1fr] items-center gap-y-4 gap-x-3">
            <label for="service_type" class="font-bold">Type</label>
            <USelectMenu
              id="service_type"
              v-model="service.service_type"
              :disabled="!canWrite"
              :items="serviceTypeOptions"
              value-key="value"
              label-key="label"
              placeholder="Not set"
              class="w-full"
            />

            <label for="bandwidth_mbps" class="font-bold">Bandwidth (Mbps)</label>
            <UInputNumber
              id="bandwidth_mbps"
              v-model="service.bandwidth_mbps"
              :disabled="!canWrite"
              :min="0"
              class="w-full"
            />

            <template v-if="service.service_type === 'ELAN'">
              <label for="max_mac_addresses" class="font-bold">Max MAC addresses</label>
              <UInputNumber
                id="max_mac_addresses"
                v-model="service.max_mac_addresses"
                :disabled="!canWrite"
                :min="0"
                class="w-full"
              />
            </template>
          </div>
        </div>

        <template v-if="genericRoles.length">
          <hr class="my-6" />
          <div class="flex flex-col gap-4">
            <h5 class="m-0">Endpoints</h5>
            <div
              v-if="service.pseudowire_id"
              class="grid grid-cols-[9rem_1fr] items-center gap-x-3"
            >
              <label class="font-bold">Pseudowire ID</label>
              <UInput :model-value="service.pseudowire_id" disabled class="w-full" />
            </div>
            <div
              v-for="(ep, i) in genericEndpoints"
              :key="i"
              class="flex flex-col gap-2 border border-default rounded p-3"
            >
              <div class="grid grid-cols-[9rem_1fr] items-center gap-y-3 gap-x-3">
                <label class="font-bold">Role</label>
                <USelectMenu
                  v-model="ep.role"
                  :disabled="!canWrite"
                  :items="genericRoles.map((r) => ({ label: r.name, value: r.name }))"
                  value-key="value"
                  label-key="label"
                />
                <label class="font-bold">Device / interface</label>
                <div class="flex items-center gap-2">
                  <UInput
                    :model-value="ep.label"
                    disabled
                    placeholder="Not selected"
                    class="w-full"
                  />
                  <UButton
                    icon="i-lucide-list-tree"
                    variant="outline"
                    color="neutral"
                    :disabled="!canWrite"
                    @click="openGenericPicker(i)"
                  />
                </div>
                <template
                  v-for="field in genericRoles.find((r) => r.name === ep.role)?.fields ?? []"
                  :key="field.name"
                >
                  <label class="font-bold">{{ field.name }}</label>
                  <UInput v-model="ep.fields[field.name]" :disabled="!canWrite" />
                </template>
              </div>
              <div class="flex justify-end">
                <UButton
                  v-if="canWrite"
                  label="Remove"
                  variant="ghost"
                  color="error"
                  size="sm"
                  @click="removeGenericEndpoint(i)"
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
            <div class="flex justify-end gap-2">
              <UButton
                label="Save endpoints"
                variant="outline"
                :loading="genericSaving"
                :disabled="!canWrite"
                @click="saveGenericEndpoints"
              />
              <UButton
                label="Save & push"
                icon="i-lucide-cloud-upload"
                :loading="genericSaving || genericPushing"
                :disabled="!canWrite"
                @click="saveAndPushGeneric"
              />
            </div>
            <div v-if="genericPushResults.length" class="flex flex-col gap-1">
              <div
                v-for="result in genericPushResults"
                :key="result.device"
                class="flex items-center gap-2 text-sm"
              >
                <UBadge
                  :label="result.error ? 'Failed' : 'OK'"
                  :color="result.error ? 'error' : 'success'"
                  variant="subtle"
                />
                <span class="font-medium">{{ result.device }}</span>
                <span v-if="result.error" class="text-red-500">{{ result.error }}</span>
              </div>
            </div>
          </div>
        </template>

        <template v-if="authStore.opticalEnabled && opticalCat">
          <hr class="my-6" />
          <div class="flex flex-col gap-3">
            <h5 class="m-0">Optical / fiber path</h5>
            <div v-if="path" class="text-sm">
              Status: {{ path.status }} · {{ path.hops?.length || 0 }} hops
              <ul class="mt-2">
                <li v-for="h in path.hops ?? []" :key="h.seq">
                  {{ h.seq }}. {{ h.kind }} — {{ h.label }}
                </li>
              </ul>
            </div>
            <div class="flex flex-wrap gap-2">
              <UButton
                label="Pick A"
                variant="outline"
                :disabled="!canWrite"
                @click="pathPicker = 'a'"
              />
              <span>{{ pathA || 'A not set' }}</span>
              <UButton
                label="Pick Z"
                variant="outline"
                :disabled="!canWrite"
                @click="pathPicker = 'z'"
              />
              <span>{{ pathZ || 'Z not set' }}</span>
              <UButton
                label="Trace & attach"
                :disabled="!canWrite || !pathA || !pathZ"
                @click="attachPath"
              />
            </div>
          </div>
        </template>
      </template>
    </template>

    <template #footer>
      <div class="flex w-full justify-between">
        <UButton
          v-if="service.source === 'factum' && canWrite"
          label="Delete"
          icon="i-lucide-trash-2"
          variant="outline"
          color="error"
          @click="confirmDelete(service)"
        />
        <div class="flex gap-2 ms-auto">
          <UButton label="Cancel" icon="i-lucide-x" variant="ghost" @click="hideDialog" />
          <UButton
            v-if="canWrite"
            label="Save"
            icon="i-lucide-check"
            :loading="saving"
            :disabled="loading"
            @click="saveService"
          />
        </div>
      </div>
    </template>
  </UModal>

  <DeviceInterfacePicker v-model:open="pickerOpen" @select="onPickerSelect" />
  <DeviceInterfacePicker
    :open="!!pathPicker"
    :mode="pathMode"
    @update:open="
      (v) => {
        if (!v) pathPicker = null
      }
    "
    @select="onPathPick"
  />

  <UModal
    v-model:open="credentialsDialog"
    title="Device credentials"
    :ui="{ content: 'sm:max-w-sm' }"
    @update:open="(isOpen) => !isOpen && cancelCredentials()"
  >
    <template #body>
      <div class="flex flex-col gap-3">
        <div class="flex flex-col gap-1">
          <label for="eline-prompt-username" class="text-sm text-muted-color">Username</label>
          <UInput
            id="eline-prompt-username"
            v-model="promptUsername"
            autocomplete="off"
            autofocus
            class="w-full"
            @keyup.enter="submitCredentials"
          />
        </div>
        <div class="flex flex-col gap-1">
          <label for="eline-prompt-password" class="text-sm text-muted-color">Password</label>
          <PasswordInput
            id="eline-prompt-password"
            v-model="promptPassword"
            autocomplete="new-password"
            @keyup.enter="submitCredentials"
          />
        </div>
      </div>
    </template>

    <template #footer>
      <UButton label="Cancel" icon="i-lucide-x" variant="ghost" @click="cancelCredentials" />
      <UButton
        label="Continue"
        icon="i-lucide-check"
        :disabled="!promptUsername || !promptPassword"
        @click="submitCredentials"
      />
    </template>
  </UModal>

  <UModal v-model:open="deleteDialog" title="Delete service" :ui="{ content: 'sm:max-w-md' }">
    <template #body>
      <p>
        Are you sure you want to delete service
        <strong>{{ deleteTarget?.service_id }}</strong
        >? This cannot be undone.
      </p>

      <div
        v-if="
          deleteTarget?.service_type === 'ELINE' &&
          (deleteTarget?.l2vpn_netbox_id || deleteTarget?.applied_to_device)
        "
        class="flex flex-col gap-2 mt-4"
      >
        <UCheckbox
          v-if="deleteTarget?.l2vpn_netbox_id"
          v-model="deleteRemoveNetbox"
          label="Also remove the L2VPN, subinterfaces and terminations from NetBox"
        />
        <UCheckbox
          v-if="deleteTarget?.applied_to_device"
          v-model="deleteRemoveDevice"
          label="Also remove the pseudowire/patch config from the endpoint device(s)"
        />
      </div>
    </template>

    <template #footer>
      <UButton label="Cancel" icon="i-lucide-x" variant="ghost" @click="hideDeleteDialog" />
      <UButton
        label="Delete"
        icon="i-lucide-trash-2"
        color="error"
        :loading="deleting"
        @click="deleteServiceConfirmed"
      />
    </template>
  </UModal>
</template>
