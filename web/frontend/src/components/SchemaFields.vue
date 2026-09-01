<script setup>
import { computed } from 'vue'

const props = defineProps({
  fields: { type: Array, default: () => [] },
  disabled: { type: Boolean, default: false },
  submitted: { type: Boolean, default: false },
})

const model = defineModel({ type: Object, default: () => ({}) })

const visible = computed(() => (props.fields ?? []).filter((f) => f?.name))

function isNumeric(field) {
  return field.type === 'int' || field.type === 'vlan'
}

function labelOf(field) {
  return field.description || field.name
}

function emptyRequired(field) {
  if (!field.required) return false
  const v = model.value?.[field.name]
  return v === null || v === undefined || v === ''
}

function onNumber(field, value) {
  model.value = { ...model.value, [field.name]: value }
}

function onText(field, value) {
  model.value = { ...model.value, [field.name]: value }
}

function onBool(field, value) {
  model.value = { ...model.value, [field.name]: Boolean(value) }
}
</script>

<template>
  <div v-for="field in visible" :key="field.name">
    <label class="block font-bold mb-2">{{ labelOf(field) }}</label>
    <UCheckbox
      v-if="field.type === 'bool'"
      :model-value="Boolean(model[field.name])"
      :disabled="disabled"
      :label="field.name"
      @update:model-value="onBool(field, $event)"
    />
    <UInputNumber
      v-else-if="isNumeric(field)"
      :model-value="model[field.name] ?? null"
      :disabled="disabled"
      :min="field.type === 'vlan' ? 1 : undefined"
      :max="field.type === 'vlan' ? 4094 : undefined"
      :color="submitted && emptyRequired(field) ? 'error' : undefined"
      :highlight="submitted && emptyRequired(field)"
      class="w-full"
      @update:model-value="onNumber(field, $event)"
    />
    <UInput
      v-else
      :model-value="model[field.name] ?? ''"
      :disabled="disabled"
      :color="submitted && emptyRequired(field) ? 'error' : undefined"
      :highlight="submitted && emptyRequired(field)"
      class="w-full"
      @update:model-value="onText(field, $event)"
    />
    <small v-if="submitted && emptyRequired(field)" class="text-red-500">
      {{ labelOf(field) }} is required.
    </small>
  </div>
</template>
