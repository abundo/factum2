<script setup>
import { computed, ref } from 'vue'
import { useToast } from '@nuxt/ui/composables'
import DeviceInterfacePicker from '@/components/DeviceInterfacePicker.vue'
import SchemaFields from '@/components/SchemaFields.vue'

defineOptions({ name: 'HomogeneousInterfaces' })

const props = defineProps({
  spec: { type: Object, default: () => ({ min: 0, max: 0, unique: false, fields: [] }) },
  disabled: { type: Boolean, default: false },
  submitted: { type: Boolean, default: false },
})

const endpoints = defineModel({ type: Array, default: () => [] })

const toast = useToast()
const pickerOpen = ref(false)
const pickerIndex = ref(null)

const min = computed(() => props.spec?.min ?? 0)
const max = computed(() => props.spec?.max ?? 0)
const unique = computed(() => !!props.spec?.unique)
const ifaceFields = computed(() => props.spec?.fields ?? [])
const canAdd = computed(() => max.value === 0 || endpoints.value.length < max.value)
const canRemove = computed(() => endpoints.value.length > min.value)

function emptyEndpoint() {
  return { role: 'interface', device_id: null, interface_id: null, fields: {}, label: '' }
}

function addEndpoint() {
  if (!canAdd.value) return
  endpoints.value = [...endpoints.value, emptyEndpoint()]
}

function removeEndpoint(i) {
  if (!canRemove.value) return
  const next = [...endpoints.value]
  next.splice(i, 1)
  endpoints.value = next
}

function openPicker(i) {
  pickerIndex.value = i
  pickerOpen.value = true
}

function samePair(a, b) {
  return a?.device_id && a?.interface_id && a.device_id === b.device_id && a.interface_id === b.interface_id
}

function onPickerSelect({ deviceId, deviceName, interfaceId, interfaceName }) {
  const i = pickerIndex.value
  const ep = endpoints.value[i]
  if (!ep) return
  const next = { ...ep, device_id: deviceId, interface_id: interfaceId, label: `${deviceName} / ${interfaceName}` }
  if (unique.value) {
    const clash = endpoints.value.some((other, j) => j !== i && samePair(other, next))
    if (clash) {
      toast.add({
        color: 'error',
        title: 'Interface already used',
        description: 'This definition requires unique device + interface pairs.',
      })
      return
    }
  }
  const list = [...endpoints.value]
  list[i] = next
  endpoints.value = list
}

function setFields(i, fields) {
  const list = [...endpoints.value]
  list[i] = { ...list[i], fields }
  endpoints.value = list
}

const pickerDeviceId = computed(() => endpoints.value[pickerIndex.value]?.device_id ?? null)
const pickerInterfaceId = computed(() => endpoints.value[pickerIndex.value]?.interface_id ?? null)
</script>

<template>
  <div class="flex flex-col gap-3">
    <div
      v-for="(ep, i) in endpoints"
      :key="i"
      class="border border-default rounded p-3 flex flex-col gap-2"
    >
      <div class="font-bold">Interface {{ i + 1 }}</div>
      <div>
        <label class="mb-1 block font-bold">Device / interface</label>
        <div class="flex items-center gap-2">
          <UInput :model-value="ep.label" disabled placeholder="Not selected" class="w-full" />
          <UButton
            v-if="!disabled"
            icon="i-lucide-list-tree"
            variant="outline"
            color="neutral"
            @click="openPicker(i)"
          />
        </div>
      </div>
      <SchemaFields
        v-if="ifaceFields.length"
        :model-value="ep.fields || {}"
        :fields="ifaceFields"
        :disabled="disabled"
        :submitted="submitted"
        :interface-id="ep.interface_id"
        :device-id="ep.device_id"
        @update:model-value="setFields(i, $event)"
      />
      <div v-if="!disabled && canRemove" class="flex justify-end">
        <UButton
          label="Remove"
          variant="ghost"
          color="error"
          size="sm"
          @click="removeEndpoint(i)"
        />
      </div>
    </div>
    <div v-if="!disabled && canAdd">
      <UButton
        label="Add interface"
        variant="outline"
        color="neutral"
        size="sm"
        icon="i-lucide-plus"
        @click="addEndpoint"
      />
    </div>
    <DeviceInterfacePicker
      v-model:open="pickerOpen"
      mode="service"
      :device-id="pickerDeviceId"
      :interface-id="pickerInterfaceId"
      @select="onPickerSelect"
    />
  </div>
</template>
