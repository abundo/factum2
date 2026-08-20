<script setup>
import { useToast } from '@nuxt/ui/composables'
import { computed, ref, watch } from 'vue'
import { getDevice, getDevices } from '@/api/devices'

const props = defineProps({
  mode: { type: String, default: 'eline' }, // eline | wavelength | fiber
})

const open = defineModel('open', { type: Boolean, default: false })

const emit = defineEmits(['select'])

const toast = useToast()

const devices = ref([])
const loadingDevices = ref(false)
const deviceId = ref(null)

const device = ref(null)
const loadingInterfaces = ref(false)
const interfaceId = ref(null)

// Only Arista EOS, Nokia SR OS and Cisco IOS-XR devices can terminate an
// ELINE endpoint today. Device platform names come from Netbox (e.g. "EOS",
// "SROS-MD") so compare case-insensitively - same normalization
// internal/drivers.NewDriver does (strings.ToLower(device.Platform)) before
// matching its own driver registry, which also has more platforms than we
// want to expose here.
const supportedPlatforms = ['eos', 'sros', 'sros-md', 'ios-xr']
const supportedDevices = computed(() => {
  if (props.mode !== 'eline') return devices.value
  return devices.value.filter((d) => supportedPlatforms.includes(d.platform?.toLowerCase()))
})
const deviceOptions = computed(() =>
  supportedDevices.value.map((d) => ({ id: d.id, name: d.name })),
)

// Only physical interfaces can terminate an ELINE endpoint - same split as
// the backend's isPhysicalInterfaceType (web/handler_service_eline.go).
const physicalInterfaces = computed(() => {
  if (!device.value) return []
  return (device.value.interfaces ?? []).filter(
    (i) => i.type && i.type !== 'virtual' && i.type !== 'lag',
  )
})
const interfaceOptions = computed(() =>
  physicalInterfaces.value.map((i) => ({
    id: i.id,
    name: i.name,
    description: i.description,
    label: i.description ? `${i.name} - ${i.description}` : i.name,
  })),
)

function loadDevices() {
  loadingDevices.value = true
  getDevices()
    .then((data) => {
      devices.value = data ?? []
    })
    .catch(() => {
      toast.add({
        color: 'error',
        title: 'Error',
        description: 'Failed to load devices.',
        duration: 3000,
      })
    })
    .finally(() => {
      loadingDevices.value = false
    })
}

watch(deviceId, (id) => {
  interfaceId.value = null
  device.value = null
  if (!id) return
  loadingInterfaces.value = true
  getDevice(id)
    .then((data) => {
      device.value = data
    })
    .catch(() => {
      toast.add({
        color: 'error',
        title: 'Error',
        description: 'Failed to load interfaces.',
        duration: 3000,
      })
    })
    .finally(() => {
      loadingInterfaces.value = false
    })
})

watch(open, (isOpen) => {
  if (!isOpen) return
  deviceId.value = null
  interfaceId.value = null
  device.value = null
  if (devices.value.length === 0) loadDevices()
})

function confirmSelection() {
  if (!deviceId.value || !interfaceId.value) return
  const selectedDevice = devices.value.find((d) => d.id === deviceId.value)
  const selectedInterface = physicalInterfaces.value.find((i) => i.id === interfaceId.value)
  emit('select', {
    deviceId: deviceId.value,
    deviceName: selectedDevice?.name ?? '',
    interfaceId: interfaceId.value,
    interfaceName: selectedInterface?.name ?? '',
  })
  open.value = false
}
</script>

<template>
  <UModal v-model:open="open" title="Select device / interface" :ui="{ content: 'sm:max-w-md' }">
    <template #body>
      <div class="flex flex-col gap-4">
        <div>
          <label class="block font-bold mb-2">Device</label>
          <USelectMenu
            v-model="deviceId"
            :items="deviceOptions"
            value-key="id"
            label-key="name"
            :loading="loadingDevices"
            placeholder="Select a device"
            class="w-full"
          />
        </div>
        <div v-if="deviceId">
          <label class="block font-bold mb-2">Interface</label>
          <USelectMenu
            v-model="interfaceId"
            :items="interfaceOptions"
            value-key="id"
            label-key="label"
            :loading="loadingInterfaces"
            :disabled="loadingInterfaces"
            placeholder="Select a physical interface"
            class="w-full"
          />
          <small
            v-if="!loadingInterfaces && interfaceOptions.length === 0"
            class="text-muted block mt-1"
          >
            No physical interfaces found on this device.
          </small>
        </div>
      </div>
    </template>

    <template #footer>
      <UButton label="Cancel" icon="i-lucide-x" variant="ghost" @click="open = false" />
      <UButton
        label="Select"
        icon="i-lucide-check"
        :disabled="!deviceId || !interfaceId"
        @click="confirmSelection"
      />
    </template>
  </UModal>
</template>
