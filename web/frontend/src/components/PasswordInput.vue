<script setup>
import { ref } from 'vue'

defineProps({
  id: { type: String, default: undefined },
  placeholder: { type: String, default: undefined },
  color: { type: String, default: undefined },
  highlight: { type: Boolean, default: false },
})
const modelValue = defineModel({ type: String, default: '' })

const show = ref(false)
</script>

<template>
  <UInput
    :id="id"
    v-model="modelValue"
    :type="show ? 'text' : 'password'"
    :placeholder="placeholder"
    :color="color"
    :highlight="highlight"
    class="w-full"
    :ui="{ trailing: 'pe-1' }"
  >
    <template #trailing>
      <UButton
        color="neutral"
        variant="link"
        size="sm"
        :icon="show ? 'i-lucide-eye-off' : 'i-lucide-eye'"
        :aria-label="show ? 'Hide password' : 'Show password'"
        :aria-pressed="show"
        @click="show = !show"
      />
    </template>
  </UInput>
</template>
