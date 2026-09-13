<script setup>
import { computed } from 'vue'
import SchemaFieldControl from '@/components/SchemaFieldControl.vue'

const props = defineProps({
  fields: { type: Array, default: () => [] },
  disabled: { type: Boolean, default: false },
  submitted: { type: Boolean, default: false },
  interfaceId: { type: Number, default: null },
  deviceId: { type: Number, default: null },
})

const model = defineModel({ type: Object, default: () => ({}) })

const visible = computed(() => (props.fields ?? []).filter((f) => f?.name))

function onValue(field, value) {
  model.value = { ...model.value, [field.name]: value }
}
</script>

<template>
  <div class="flex flex-col gap-3">
    <SchemaFieldControl
      v-for="field in visible"
      :key="field.name"
      :field="field"
      :model-value="model[field.name]"
      :disabled="disabled"
      :submitted="submitted"
      :interface-id="interfaceId"
      :device-id="deviceId"
      @update:model-value="onValue(field, $event)"
    />
  </div>
</template>
