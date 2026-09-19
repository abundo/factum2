<script setup>
import { computed, watch } from 'vue'
import { parseInterfaceNamePattern } from '@/utils/interfaceNames'
import { useInterfaceTypes } from '@/composables/useInterfaceTypes'

defineOptions({ name: 'InterfaceEditorDialog' })

const open = defineModel('open', { type: Boolean })
const form = defineModel('form', { type: Object })

const { defaultType, load: loadTypes, typeItems } = useInterfaceTypes()
const interfaceTypeItems = computed(() => typeItems(form.value?.type))

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

const RANGE_HINT = 'Use Ethernet[1-48] to create many ports at once.'

const parsedName = computed(() =>
  parseInterfaceNamePattern(form.value?.name ?? '', { expandRanges: !props.editing }),
)

const nameDescription = computed(() => {
  if (props.editing || fieldsLocked.value) return ''
  const raw = form.value?.name ?? ''
  if (!raw.trim()) return RANGE_HINT
  const { names, error } = parsedName.value
  if (error && raw.includes('[')) return error
  if (names.length > 1) {
    return `Creates ${names[0]} through ${names[names.length - 1]} (${names.length} interfaces)`
  }
  return RANGE_HINT
})

const createLabel = computed(() => {
  if (props.editing) return 'Save'
  const n = parsedName.value.names.length
  return n > 1 ? `Create ${n}` : 'Create'
})

watch(open, (isOpen) => {
  if (!isOpen) return
  loadTypes().then(() => {
    if (props.editing || !form.value) return
    const current = form.value.type
    const known = interfaceTypeItems.value.some((i) => i.value === current)
    if (!known && defaultType.value) {
      form.value.type = defaultType.value
    }
  })
})
</script>

<template>
  <FormModal v-model:open="open" :source="form" :title="title" :ui="{ content: 'sm:max-w-sm' }">
    <template #body>
      <div class="flex flex-col gap-4">
        <UFormField label="Name" :description="nameDescription">
          <UInput v-model="form.name" class="w-full font-mono" autofocus :disabled="fieldsLocked" />
        </UFormField>
        <UFormField label="Type">
          <USelectMenu
            v-model="form.type"
            :items="interfaceTypeItems"
            value-key="value"
            label-key="label"
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
        :label="createLabel"
        icon="i-lucide-check"
        :loading="saving"
        @click="emit('save')"
      />
    </template>
  </FormModal>
</template>
