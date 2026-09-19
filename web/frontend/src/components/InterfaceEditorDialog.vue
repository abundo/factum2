<script setup>
import { computed } from 'vue'
import { interfaceTypeItems } from '@/utils/interfaceTypes'

defineOptions({ name: 'InterfaceEditorDialog' })

const open = defineModel('open', { type: Boolean })
const form = defineModel('form', { type: Object })

const props = defineProps({
  title: { type: String, required: true },
  kind: { type: String, default: 'device' },
  writable: { type: Boolean, default: true },
  saving: { type: Boolean, default: false },
  editing: { type: Boolean, default: false },
  canWrite: { type: Boolean, default: true },
})

const emit = defineEmits(['save'])

const fieldsLocked = computed(() => props.editing && !props.writable)
const showDeviceFields = computed(() => props.kind === 'device')
</script>

<template>
  <FormModal v-model:open="open" :source="form" :title="title" :ui="{ content: 'sm:max-w-sm' }">
    <template #body>
      <div class="flex flex-col gap-4">
        <UFormField label="Name">
          <UInput v-model="form.name" class="w-full font-mono" autofocus :disabled="fieldsLocked" />
        </UFormField>
        <UFormField label="Type">
          <USelect
            v-model="form.type"
            :items="interfaceTypeItems"
            class="w-full"
            :disabled="fieldsLocked"
          />
        </UFormField>
        <UFormField label="Label">
          <UInput v-model="form.label" class="w-full" :disabled="fieldsLocked" />
        </UFormField>
        <UFormField label="Description">
          <UInput v-model="form.description" class="w-full" :disabled="fieldsLocked" />
        </UFormField>
        <UFormField v-if="showDeviceFields" label="VRF">
          <UInput v-model="form.vrf" class="w-full" :disabled="fieldsLocked" />
        </UFormField>
        <UFormField v-if="showDeviceFields" label="Enabled">
          <USwitch v-model="form.enabled" :disabled="fieldsLocked" />
        </UFormField>
      </div>
    </template>
    <template #footer>
      <UButton label="Cancel" icon="i-lucide-x" variant="ghost" @click="open = false" />
      <UButton
        v-if="canWrite && writable"
        :label="editing ? 'Save' : 'Create'"
        icon="i-lucide-check"
        :loading="saving"
        @click="emit('save')"
      />
    </template>
  </FormModal>
</template>
