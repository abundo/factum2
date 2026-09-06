<script setup>
import { useToast } from '@nuxt/ui/composables'
import { computed, nextTick, ref, watch } from 'vue'
import { getDevice, getDevices } from '@/api/devices'
import SearchInput from '@/components/SearchInput.vue'
import SortableColumnHeader from '@/components/SortableColumnHeader.vue'

const props = defineProps({
  mode: { type: String, default: 'eline' }, // eline | wavelength | fiber
  // Currently assigned pair, used to preselect rows when the modal opens.
  deviceId: { type: Number, default: null },
  interfaceId: { type: Number, default: null },
})

const open = defineModel('open', { type: Boolean, default: false })

const emit = defineEmits(['select'])

const toast = useToast()

const devices = ref([])
const loadingDevices = ref(false)
const selectedDeviceId = ref(null)

const device = ref(null)
const loadingInterfaces = ref(false)
const selectedInterfaceId = ref(null)

const deviceFilter = ref('')
const interfaceFilter = ref('')
const deviceSorting = ref([{ id: 'name', desc: false }])
const interfaceSorting = ref([{ id: 'name', desc: false }])
const deviceTable = ref(null)
const interfaceTable = ref(null)

const deviceColumns = [
  { accessorKey: 'name', header: 'Name' },
  { accessorKey: 'site', header: 'Site' },
  { accessorKey: 'platform', header: 'Platform' },
]
const interfaceColumns = [
  { accessorKey: 'name', header: 'Name' },
  { accessorKey: 'description', header: 'Description' },
]

// Default UTable selected rows use the same bg-elevated/50 as hover, so a
// click is almost invisible. Primary fill + inset bar beat both the default
// selected style and the selectable-row hover.
const pickerTableUi = {
  th: 'px-3 py-2',
  td: 'px-3 py-2',
  tbody: '[&>tr[data-selected=true]]:bg-primary/25 [&>tr[data-selected=true]]:hover:bg-primary/30',
  tr: [
    'data-[selected=true]:bg-primary/25',
    'data-[selected=true]:hover:bg-primary/30',
    'data-[selected=true]:shadow-[inset_3px_0_0_0_var(--ui-primary)]',
    '[&[data-selected=true]>td]:text-highlighted',
    '[&[data-selected=true]>td]:font-medium',
  ].join(' '),
}

// Only Arista EOS, Nokia SR OS and Cisco IOS-XR devices can terminate an
// ELINE endpoint today. Device platform names come from Netbox (e.g. "EOS",
// "SROS-MD") so compare case-insensitively - same normalization
// internal/drivers.NewDriver does (strings.ToLower(device.Platform)) before
// matching its own driver registry, which also has more platforms than we
// want to expose here. An already-assigned device is kept in the list even
// if its platform would not be offered for a new pick.
const supportedPlatforms = ['eos', 'sros', 'sros-md', 'ios-xr']
const supportedDevices = computed(() => {
  if (props.mode !== 'eline') return devices.value
  return devices.value.filter(
    (d) =>
      supportedPlatforms.includes(d.platform?.toLowerCase()) || d.id === selectedDeviceId.value,
  )
})

const deviceRowSelection = computed(() =>
  selectedDeviceId.value ? { [String(selectedDeviceId.value)]: true } : {},
)
const interfaceRowSelection = computed(() =>
  selectedInterfaceId.value ? { [String(selectedInterfaceId.value)]: true } : {},
)

// Only physical interfaces can terminate an ELINE endpoint - same split as
// the backend's isPhysicalInterfaceType (web/handler_service_eline.go). An
// already-assigned interface is kept visible even if it would be filtered.
const physicalInterfaces = computed(() => {
  if (!device.value) return []
  return (device.value.interfaces ?? []).filter(
    (i) =>
      (i.type && i.type !== 'virtual' && i.type !== 'lag') || i.id === selectedInterfaceId.value,
  )
})

function rowId(row) {
  return String(row.id)
}

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

function loadInterfaces(id) {
  if (!id) {
    device.value = null
    loadingInterfaces.value = false
    return
  }
  loadingInterfaces.value = true
  device.value = null
  getDevice(id)
    .then((data) => {
      if (selectedDeviceId.value !== id) return
      device.value = data
      if (
        selectedInterfaceId.value &&
        !(data.interfaces ?? []).some((i) => i.id === selectedInterfaceId.value)
      ) {
        selectedInterfaceId.value = null
      }
    })
    .catch(() => {
      if (selectedDeviceId.value !== id) return
      toast.add({
        color: 'error',
        title: 'Error',
        description: 'Failed to load interfaces.',
        duration: 3000,
      })
    })
    .finally(() => {
      if (selectedDeviceId.value === id) loadingInterfaces.value = false
    })
}

function selectDevice(id, { keepInterface = false } = {}) {
  selectedDeviceId.value = id
  if (!keepInterface) selectedInterfaceId.value = null
  loadInterfaces(id)
}

function onDeviceSelect(_e, row) {
  if (row.original.id === selectedDeviceId.value) return
  selectDevice(row.original.id)
}

function onInterfaceSelect(_e, row) {
  selectedInterfaceId.value = row.original.id
}

function scrollSelectedRow(tableRef) {
  nextTick(() => {
    const root = tableRef.value?.$el ?? tableRef.value
    root?.querySelector?.('[data-selected="true"]')?.scrollIntoView({ block: 'nearest' })
  })
}

watch(open, (isOpen) => {
  if (!isOpen) return
  deviceFilter.value = ''
  interfaceFilter.value = ''
  selectDevice(props.deviceId ?? null, { keepInterface: true })
  selectedInterfaceId.value = props.interfaceId ?? null
  if (devices.value.length === 0) loadDevices()
})

watch(
  [loadingDevices, selectedDeviceId, supportedDevices],
  () => {
    if (!loadingDevices.value && selectedDeviceId.value) scrollSelectedRow(deviceTable)
  },
  { flush: 'post' },
)

watch(
  [loadingInterfaces, selectedInterfaceId, physicalInterfaces],
  () => {
    if (!loadingInterfaces.value && selectedInterfaceId.value) scrollSelectedRow(interfaceTable)
  },
  { flush: 'post' },
)

function confirmSelection() {
  if (!selectedDeviceId.value || !selectedInterfaceId.value) return
  const selectedDevice = devices.value.find((d) => d.id === selectedDeviceId.value)
  const selectedInterface = physicalInterfaces.value.find((i) => i.id === selectedInterfaceId.value)
  emit('select', {
    deviceId: selectedDeviceId.value,
    deviceName: selectedDevice?.name ?? '',
    interfaceId: selectedInterfaceId.value,
    interfaceName: selectedInterface?.name ?? '',
  })
  open.value = false
}
</script>

<template>
  <UModal v-model:open="open" title="Select device / interface" :ui="{ content: 'sm:max-w-5xl' }">
    <template #body>
      <div class="grid grid-cols-1 md:grid-cols-2 gap-4">
        <div class="flex flex-col gap-2 min-w-0">
          <div class="flex items-center justify-between gap-2">
            <label class="font-bold">Device</label>
            <SearchInput v-model="deviceFilter" size="sm" class="w-48" />
          </div>
          <UTable
            ref="deviceTable"
            v-model:sorting="deviceSorting"
            v-model:global-filter="deviceFilter"
            :data="supportedDevices"
            :columns="deviceColumns"
            :loading="loadingDevices"
            :row-selection="deviceRowSelection"
            :get-row-id="rowId"
            empty="No devices found."
            sticky
            class="max-h-[50vh]"
            :ui="pickerTableUi"
            @select="onDeviceSelect"
          >
            <template
              v-for="col in deviceColumns"
              :key="col.accessorKey"
              #[`${col.accessorKey}-header`]="{ column }"
            >
              <SortableColumnHeader :column="column" :label="col.header" />
            </template>
            <template #name-cell="{ row }">
              <span class="inline-flex items-center gap-2">
                <UIcon
                  v-if="row.getIsSelected()"
                  name="i-lucide-check"
                  class="size-4 text-primary shrink-0"
                />
                <span v-else class="size-4 shrink-0" />
                {{ row.original.name }}
              </span>
            </template>
          </UTable>
        </div>

        <div class="flex flex-col gap-2 min-w-0">
          <div class="flex items-center justify-between gap-2">
            <label class="font-bold">Interface</label>
            <SearchInput
              v-model="interfaceFilter"
              size="sm"
              class="w-48"
              :disabled="!selectedDeviceId"
            />
          </div>
          <UTable
            ref="interfaceTable"
            v-model:sorting="interfaceSorting"
            v-model:global-filter="interfaceFilter"
            :data="physicalInterfaces"
            :columns="interfaceColumns"
            :loading="loadingInterfaces"
            :row-selection="interfaceRowSelection"
            :get-row-id="rowId"
            :empty="
              loadingInterfaces
                ? 'Loading...'
                : selectedDeviceId
                  ? 'No physical interfaces found on this device.'
                  : 'Select a device.'
            "
            sticky
            class="max-h-[50vh]"
            :ui="pickerTableUi"
            @select="onInterfaceSelect"
          >
            <template
              v-for="col in interfaceColumns"
              :key="col.accessorKey"
              #[`${col.accessorKey}-header`]="{ column }"
            >
              <SortableColumnHeader :column="column" :label="col.header" />
            </template>
            <template #name-cell="{ row }">
              <span class="inline-flex items-center gap-2">
                <UIcon
                  v-if="row.getIsSelected()"
                  name="i-lucide-check"
                  class="size-4 text-primary shrink-0"
                />
                <span v-else class="size-4 shrink-0" />
                {{ row.original.name }}
              </span>
            </template>
          </UTable>
        </div>
      </div>
    </template>

    <template #footer>
      <UButton label="Cancel" icon="i-lucide-x" variant="ghost" @click="open = false" />
      <UButton
        label="Select"
        icon="i-lucide-check"
        :disabled="!selectedDeviceId || !selectedInterfaceId"
        @click="confirmSelection"
      />
    </template>
  </UModal>
</template>
