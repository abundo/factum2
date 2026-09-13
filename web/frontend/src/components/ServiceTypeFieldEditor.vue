<script setup>
defineOptions({ name: 'ServiceTypeFieldEditor' })

const fields = defineModel({ type: Array, default: () => [] })
defineProps({
  title: { type: String, default: 'Fields' },
})

const typeOptions = [
  { label: 'string', value: 'string' },
  { label: 'int', value: 'int' },
  { label: 'bool', value: 'bool' },
  { label: 'enum', value: 'enum' },
  { label: 'vlan', value: 'vlan' },
  { label: 'mac', value: 'mac' },
  { label: 'ipv4', value: 'ipv4' },
  { label: 'ipv6', value: 'ipv6' },
  { label: 'ip', value: 'ip' },
  { label: 'ipv4 prefix', value: 'ipv4_prefix' },
  { label: 'ipv6 prefix', value: 'ipv6_prefix' },
  { label: 'prefix', value: 'prefix' },
  { label: 'service id', value: 'service_id' },
  { label: 'list', value: 'list' },
]

const itemTypeOptions = typeOptions.filter((o) => o.value !== 'list')

function optionValue(v) {
  if (v && typeof v === 'object' && !Array.isArray(v) && 'value' in v) return v.value
  return v
}

function typeOf(field) {
  return optionValue(field?.type) || 'string'
}

function isPrefixType(type) {
  return type === 'prefix' || type === 'ipv4_prefix' || type === 'ipv6_prefix'
}

function emptyField() {
  return { name: '', type: 'string', required: false, description: '' }
}

function addField() {
  fields.value = [...(fields.value ?? []), emptyField()]
}

function removeField(i) {
  const next = [...(fields.value ?? [])]
  next.splice(i, 1)
  fields.value = next
}

function onType(field, value) {
  const type = optionValue(value) || 'string'
  field.type = type
  if (type === 'list') {
    if (!field.items) field.items = { type: 'string' }
  } else {
    field.items = undefined
  }
  if (type !== 'enum') field.enum = undefined
  if (type !== 'int' && type !== 'vlan' && type !== 'list') {
    field.min = undefined
    field.max = undefined
  }
  if (type !== 'int') field.unit = undefined
  if (type !== 'bool') {
    field.bool_true_label = undefined
    field.bool_false_label = undefined
  }
  if (!isPrefixType(type)) field.resource = undefined
}

function onItemType(field, value) {
  if (!field.items) field.items = {}
  const type = optionValue(value) || 'string'
  field.items.type = type
  if (type !== 'enum') field.items.enum = undefined
  if (type !== 'int' && type !== 'vlan') {
    field.items.min = undefined
    field.items.max = undefined
  }
  if (type !== 'int') field.items.unit = undefined
  if (type !== 'bool') {
    field.items.bool_true_label = undefined
    field.items.bool_false_label = undefined
  }
  if (!isPrefixType(type)) field.items.resource = undefined
}

function addEnum(target) {
  const cur = [...(target.enum ?? [])]
  cur.push({ label: '', value: '' })
  target.enum = cur
}

function removeEnum(target, i) {
  const cur = [...(target.enum ?? [])]
  cur.splice(i, 1)
  target.enum = cur
}
</script>

<template>
  <div class="flex flex-col gap-2">
    <div class="flex items-center justify-between">
      <label class="block font-bold m-0">{{ title }}</label>
      <UButton
        icon="i-lucide-plus"
        size="xs"
        variant="outline"
        label="Add field"
        @click="addField"
      />
    </div>
    <p v-if="!(fields ?? []).length" class="text-muted-color text-sm m-0">No fields.</p>
    <div
      v-for="(field, i) in fields"
      :key="i"
      class="rounded-md ring ring-default p-3 flex flex-col gap-2"
    >
      <div class="flex gap-2 items-start">
        <div class="flex-1 min-w-0">
          <label class="block font-bold mb-1 text-sm">Name</label>
          <UInput
            v-model="field.name"
            placeholder="bandwidth_mbps"
            class="w-full font-mono text-sm"
          />
        </div>
        <div class="w-40 shrink-0">
          <label class="block font-bold mb-1 text-sm">Type</label>
          <USelectMenu
            :model-value="typeOf(field)"
            :items="typeOptions"
            value-key="value"
            label-key="label"
            class="w-full"
            @update:model-value="onType(field, $event)"
          />
        </div>
        <UButton
          icon="i-lucide-trash-2"
          variant="ghost"
          color="error"
          size="sm"
          class="mt-6"
          @click="removeField(i)"
        />
      </div>
      <div>
        <label class="block font-bold mb-1 text-sm">Description</label>
        <UInput v-model="field.description" class="w-full" />
      </div>
      <label class="flex items-center gap-2 text-sm">
        <UCheckbox v-model="field.required" />
        Required
      </label>
      <div v-if="typeOf(field) === 'int'" class="grid grid-cols-3 gap-2">
        <div>
          <label class="block font-bold mb-1 text-sm">Min</label>
          <UInput v-model="field.min" type="number" class="w-full" />
        </div>
        <div>
          <label class="block font-bold mb-1 text-sm">Max</label>
          <UInput v-model="field.max" type="number" class="w-full" />
        </div>
        <div>
          <label class="block font-bold mb-1 text-sm">Unit</label>
          <UInput v-model="field.unit" placeholder="Mbps" class="w-full" />
        </div>
      </div>
      <div v-else-if="typeOf(field) === 'vlan'" class="grid grid-cols-2 gap-2">
        <div>
          <label class="block font-bold mb-1 text-sm">Min</label>
          <UInput v-model="field.min" type="number" placeholder="1" class="w-full" />
        </div>
        <div>
          <label class="block font-bold mb-1 text-sm">Max</label>
          <UInput v-model="field.max" type="number" placeholder="4094" class="w-full" />
        </div>
      </div>
      <div v-else-if="typeOf(field) === 'bool'" class="grid grid-cols-2 gap-2">
        <div>
          <label class="block font-bold mb-1 text-sm">True label</label>
          <UInput v-model="field.bool_true_label" placeholder="Yes" class="w-full" />
        </div>
        <div>
          <label class="block font-bold mb-1 text-sm">False label</label>
          <UInput v-model="field.bool_false_label" placeholder="No" class="w-full" />
        </div>
      </div>
      <div v-else-if="typeOf(field) === 'enum'" class="flex flex-col gap-2">
        <div class="flex items-center justify-between">
          <label class="block font-bold m-0 text-sm">Choices</label>
          <UButton
            icon="i-lucide-plus"
            size="xs"
            variant="ghost"
            label="Add"
            @click="addEnum(field)"
          />
        </div>
        <div v-for="(choice, j) in field.enum ?? []" :key="j" class="flex gap-2">
          <UInput v-model="choice.label" placeholder="Label" class="flex-1" />
          <UInput v-model="choice.value" placeholder="value" class="flex-1 font-mono text-sm" />
          <UButton
            icon="i-lucide-trash-2"
            variant="ghost"
            color="error"
            size="sm"
            @click="removeEnum(field, j)"
          />
        </div>
      </div>
      <div v-if="isPrefixType(typeOf(field))">
        <label class="block font-bold mb-1 text-sm">Resource (optional)</label>
        <UInput
          v-model="field.resource"
          placeholder="peering-v4"
          class="w-full font-mono text-sm"
        />
      </div>
      <div v-if="typeOf(field) === 'list'" class="rounded-md bg-muted p-2 flex flex-col gap-2">
        <div class="grid grid-cols-3 gap-2">
          <div>
            <label class="block font-bold mb-1 text-sm">Item type</label>
            <USelectMenu
              :model-value="typeOf(field.items)"
              :items="itemTypeOptions"
              value-key="value"
              label-key="label"
              class="w-full"
              @update:model-value="onItemType(field, $event)"
            />
          </div>
          <div>
            <label class="block font-bold mb-1 text-sm">Length min</label>
            <UInput v-model="field.min" type="number" class="w-full" />
          </div>
          <div>
            <label class="block font-bold mb-1 text-sm">Length max</label>
            <UInput v-model="field.max" type="number" class="w-full" />
          </div>
        </div>
        <div v-if="typeOf(field.items) === 'int'" class="grid grid-cols-3 gap-2">
          <div>
            <label class="block font-bold mb-1 text-sm">Item min</label>
            <UInput v-model="field.items.min" type="number" class="w-full" />
          </div>
          <div>
            <label class="block font-bold mb-1 text-sm">Item max</label>
            <UInput v-model="field.items.max" type="number" class="w-full" />
          </div>
          <div>
            <label class="block font-bold mb-1 text-sm">Item unit</label>
            <UInput v-model="field.items.unit" class="w-full" />
          </div>
        </div>
        <div v-else-if="typeOf(field.items) === 'vlan'" class="grid grid-cols-2 gap-2">
          <div>
            <label class="block font-bold mb-1 text-sm">Item min</label>
            <UInput v-model="field.items.min" type="number" class="w-full" />
          </div>
          <div>
            <label class="block font-bold mb-1 text-sm">Item max</label>
            <UInput v-model="field.items.max" type="number" class="w-full" />
          </div>
        </div>
        <div v-else-if="typeOf(field.items) === 'bool'" class="grid grid-cols-2 gap-2">
          <div>
            <label class="block font-bold mb-1 text-sm">True label</label>
            <UInput v-model="field.items.bool_true_label" placeholder="Yes" class="w-full" />
          </div>
          <div>
            <label class="block font-bold mb-1 text-sm">False label</label>
            <UInput v-model="field.items.bool_false_label" placeholder="No" class="w-full" />
          </div>
        </div>
        <div v-else-if="typeOf(field.items) === 'enum'" class="flex flex-col gap-2">
          <div class="flex items-center justify-between">
            <label class="block font-bold m-0 text-sm">Item choices</label>
            <UButton
              icon="i-lucide-plus"
              size="xs"
              variant="ghost"
              label="Add"
              @click="addEnum(field.items)"
            />
          </div>
          <div v-for="(choice, j) in field.items.enum ?? []" :key="j" class="flex gap-2">
            <UInput v-model="choice.label" placeholder="Label" class="flex-1" />
            <UInput v-model="choice.value" placeholder="value" class="flex-1 font-mono text-sm" />
            <UButton
              icon="i-lucide-trash-2"
              variant="ghost"
              color="error"
              size="sm"
              @click="removeEnum(field.items, j)"
            />
          </div>
        </div>
        <div v-if="isPrefixType(typeOf(field.items))">
          <label class="block font-bold mb-1 text-sm">Item resource (optional)</label>
          <UInput
            v-model="field.items.resource"
            placeholder="peering-v4"
            class="w-full font-mono text-sm"
          />
        </div>
      </div>
    </div>
  </div>
</template>
