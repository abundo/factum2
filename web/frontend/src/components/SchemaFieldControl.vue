<script setup>
import { computed, ref, watch } from 'vue'
import { getFreeResources } from '@/api/config'
import { searchCommercialServices } from '@/api/services'
import SearchInput from '@/components/SearchInput.vue'

defineOptions({ name: 'SchemaFieldControl' })

const props = defineProps({
  field: { type: Object, required: true },
  disabled: { type: Boolean, default: false },
  submitted: { type: Boolean, default: false },
  interfaceId: { type: Number, default: null },
  deviceId: { type: Number, default: null },
  hideLabel: { type: Boolean, default: false },
})

const model = defineModel({ default: undefined })

const pickerOpen = ref(false)
const pickerQ = ref('')
const pickerRows = ref([])
const pickerLoading = ref(false)
let pickerTimer = null

const allocOpen = ref(false)
const allocLoading = ref(false)
const allocError = ref('')
const allocRows = ref([])

const type = computed(() => props.field?.type || 'string')
const isNumeric = computed(() => type.value === 'int' || type.value === 'vlan')
const isPrefix = computed(
  () =>
    type.value === 'prefix' ||
    type.value === 'ipv4_prefix' ||
    type.value === 'ipv6_prefix',
)

function labelOf(field) {
  return field.description || field.name || field.type || 'Field'
}

function fieldEmpty(field, v) {
  if (v === null || v === undefined || v === '') return true
  if ((field.type === 'service_id' || type.value === 'service_id') && Number(v) === 0) return true
  return false
}

const showRequired = computed(
  () => props.submitted && props.field.required && fieldEmpty(props.field, model.value),
)

function vlanMin() {
  const n = Number(props.field.min)
  return Number.isFinite(n) ? n : 1
}
function vlanMax() {
  const n = Number(props.field.max)
  return Number.isFinite(n) ? n : 4094
}
function intMin() {
  const n = Number(props.field.min)
  return Number.isFinite(n) ? n : undefined
}
function intMax() {
  const n = Number(props.field.max)
  return Number.isFinite(n) ? n : undefined
}

const enumItems = computed(() =>
  (props.field.enum ?? []).map((e) => ({
    label: e.label || e.value,
    value: e.value,
  })),
)

const trueLabel = computed(() => props.field.bool_true_label || 'Yes')
const falseLabel = computed(() => props.field.bool_false_label || 'No')
const boolItems = computed(() => [
  { label: trueLabel.value, value: true },
  { label: falseLabel.value, value: false },
])

function canonicalizeMAC(raw) {
  const hex = String(raw ?? '')
    .replace(/[^0-9a-fA-F]/g, '')
    .toLowerCase()
  if (hex.length !== 12) return raw
  return `${hex.slice(0, 4)}.${hex.slice(4, 8)}.${hex.slice(8, 12)}`
}

function onMAC(value) {
  model.value = canonicalizeMAC(value)
}

function listValue() {
  return Array.isArray(model.value) ? model.value : []
}

function itemField() {
  const items = props.field.items ?? { type: 'string' }
  return { ...items, name: items.name || 'item', required: false }
}

function emptyItem() {
  const t = props.field.items?.type
  if (t === 'int' || t === 'vlan' || t === 'service_id') return null
  if (t === 'bool') return false
  return ''
}

function addItem() {
  model.value = [...listValue(), emptyItem()]
}

function removeItem(i) {
  const next = [...listValue()]
  next.splice(i, 1)
  model.value = next
}

function setItem(i, v) {
  const next = [...listValue()]
  next[i] = v
  model.value = next
}

const serviceLabel = computed(() => {
  const id = model.value
  if (!id) return ''
  const row = pickerRows.value.find((s) => s.id === id)
  if (row) return `${row.service_id}${row.company ? ` — ${row.company}` : ''}`
  return `#${id}`
})

function loadPicker() {
  pickerLoading.value = true
  searchCommercialServices(pickerQ.value)
    .then((rows) => {
      pickerRows.value = rows ?? []
    })
    .catch(() => {
      pickerRows.value = []
    })
    .finally(() => {
      pickerLoading.value = false
    })
}

function openPicker() {
  pickerQ.value = ''
  pickerOpen.value = true
  loadPicker()
}

watch(pickerQ, () => {
  clearTimeout(pickerTimer)
  pickerTimer = setTimeout(loadPicker, 200)
})

function pickService(row) {
  model.value = row.id
  pickerOpen.value = false
}

function clearService() {
  model.value = null
}

function familyFor() {
  if (type.value === 'ipv4_prefix' || props.field.items?.type === 'ipv4_prefix') return 4
  if (type.value === 'ipv6_prefix' || props.field.items?.type === 'ipv6_prefix') return 6
  return 0
}

function openAlloc() {
  allocOpen.value = true
  allocError.value = ''
  allocRows.value = []
  if (!props.interfaceId && !props.deviceId) {
    allocError.value = 'Select an interface first.'
    return
  }
  const name = props.field.resource
  if (!name) {
    allocError.value = 'No resource named on this field.'
    return
  }
  allocLoading.value = true
  getFreeResources({
    interface_id: props.interfaceId || undefined,
    device_id: props.deviceId || undefined,
    name,
    family: familyFor(),
  })
    .then((data) => {
      allocRows.value = data?.cidrs ?? []
    })
    .catch((err) => {
      allocError.value = err?.response?.data?.error ?? 'Failed to list free prefixes.'
    })
    .finally(() => {
      allocLoading.value = false
    })
}

function pickCIDR(row) {
  if (!row?.free) return
  model.value = row.prefix
  allocOpen.value = false
}

const showAlloc = computed(() => isPrefix.value && !!props.field.resource)
</script>

<template>
  <div>
    <label v-if="!hideLabel && field.name" class="block font-bold mb-2">{{ labelOf(field) }}</label>

    <URadioGroup
      v-if="type === 'bool'"
      :model-value="Boolean(model)"
      :items="boolItems"
      :disabled="disabled"
      value-key="value"
      label-key="label"
      @update:model-value="model = Boolean($event)"
    />

    <UInputNumber
      v-else-if="type === 'vlan'"
      :model-value="model ?? null"
      :disabled="disabled"
      :min="vlanMin()"
      :max="vlanMax()"
      :color="showRequired ? 'error' : undefined"
      :highlight="showRequired"
      class="w-full"
      @update:model-value="model = $event"
    />

    <div v-else-if="type === 'int'" class="flex items-center gap-2">
      <UInputNumber
        :model-value="model ?? null"
        :disabled="disabled"
        :min="intMin()"
        :max="intMax()"
        :color="showRequired ? 'error' : undefined"
        :highlight="showRequired"
        class="w-full"
        @update:model-value="model = $event"
      />
      <span v-if="field.unit" class="text-muted-color text-sm shrink-0">{{ field.unit }}</span>
    </div>

    <USelectMenu
      v-else-if="type === 'enum'"
      :model-value="model ?? ''"
      :items="enumItems"
      :disabled="disabled"
      value-key="value"
      label-key="label"
      :color="showRequired ? 'error' : undefined"
      class="w-full"
      @update:model-value="model = $event"
    />

    <UInput
      v-else-if="type === 'mac'"
      :model-value="model ?? ''"
      :disabled="disabled"
      placeholder="aabb.ccdd.eeff"
      :color="showRequired ? 'error' : undefined"
      :highlight="showRequired"
      class="w-full font-mono"
      @update:model-value="onMAC"
    />

    <div v-else-if="type === 'service_id'" class="flex items-center gap-2">
      <UInput
        :model-value="serviceLabel"
        disabled
        placeholder="Not selected"
        :color="showRequired ? 'error' : undefined"
        :highlight="showRequired"
        class="w-full"
      />
      <UButton
        v-if="!disabled"
        icon="i-lucide-search"
        variant="outline"
        color="neutral"
        @click="openPicker"
      />
      <UButton
        v-if="!disabled && model"
        icon="i-lucide-x"
        variant="ghost"
        color="neutral"
        @click="clearService"
      />
    </div>

    <div v-else-if="type === 'list'" class="flex flex-col gap-2">
      <div v-for="(item, i) in listValue()" :key="i" class="flex items-start gap-2">
        <SchemaFieldControl
          class="flex-1 min-w-0"
          :field="itemField()"
          :model-value="item"
          :disabled="disabled"
          :submitted="submitted"
          :interface-id="interfaceId"
          :device-id="deviceId"
          hide-label
          @update:model-value="setItem(i, $event)"
        />
        <UButton
          v-if="!disabled"
          icon="i-lucide-trash-2"
          variant="ghost"
          color="error"
          size="sm"
          class="mt-0.5"
          @click="removeItem(i)"
        />
      </div>
      <UButton
        v-if="!disabled"
        icon="i-lucide-plus"
        size="xs"
        variant="outline"
        color="neutral"
        label="Add"
        @click="addItem"
      />
    </div>

    <div v-else class="flex items-center gap-2">
      <UInput
        :model-value="model ?? ''"
        :disabled="disabled"
        :color="showRequired ? 'error' : undefined"
        :highlight="showRequired"
        :class="['w-full', isNumeric ? '' : isPrefix || type.startsWith('ip') ? 'font-mono' : '']"
        @update:model-value="model = $event"
      />
      <UButton
        v-if="showAlloc && !disabled"
        label="Allocate"
        size="sm"
        variant="outline"
        color="neutral"
        @click="openAlloc"
      />
    </div>

    <small v-if="showRequired" class="text-red-500"> {{ labelOf(field) }} is required. </small>
  </div>

  <UModal v-model:open="pickerOpen" title="Select commercial service" :ui="{ content: 'sm:max-w-lg' }">
    <template #body>
      <div class="flex flex-col gap-3">
        <SearchInput v-model="pickerQ" placeholder="Search CN/CI…" />
        <p class="text-muted-color text-sm m-0">Searches CN, CI, and free-text commercial IDs.</p>
        <div v-if="pickerLoading" class="text-muted-color text-sm">Searching…</div>
        <button
          v-for="row in pickerRows"
          :key="row.id"
          type="button"
          class="flex flex-col items-start rounded-md ring ring-default p-2 text-left hover:bg-elevated"
          @click="pickService(row)"
        >
          <span class="font-medium">{{ row.service_id }}</span>
          <span class="text-sm text-muted-color">{{
            [row.company, row.service_type].filter(Boolean).join(' · ') || 'Commercial'
          }}</span>
        </button>
        <p v-if="!pickerLoading && !pickerRows.length" class="text-muted-color text-sm m-0">
          No matching services.
        </p>
      </div>
    </template>
    <template #footer>
      <UButton label="Cancel" variant="ghost" @click="pickerOpen = false" />
    </template>
  </UModal>

  <UModal v-model:open="allocOpen" title="Allocate prefix" :ui="{ content: 'sm:max-w-md' }">
    <template #body>
      <p v-if="allocLoading" class="text-muted-color text-sm">Loading free prefixes…</p>
      <p v-else-if="allocError" class="text-red-500 text-sm m-0">{{ allocError }}</p>
      <div v-else class="flex flex-col gap-2">
        <button
          v-for="row in allocRows"
          :key="row.prefix"
          type="button"
          class="flex items-center justify-between rounded-md ring ring-default p-2 text-left font-mono text-sm"
          :class="row.free ? 'hover:bg-elevated' : 'opacity-50 cursor-not-allowed'"
          :disabled="!row.free"
          @click="pickCIDR(row)"
        >
          <span>{{ row.prefix }}</span>
          <UBadge :label="row.free ? 'free' : 'in use'" :color="row.free ? 'success' : 'neutral'" />
        </button>
        <p v-if="!allocRows.length" class="text-muted-color text-sm m-0">No prefixes on this resource.</p>
      </div>
    </template>
    <template #footer>
      <UButton label="Cancel" variant="ghost" @click="allocOpen = false" />
    </template>
  </UModal>
</template>
