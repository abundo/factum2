<script setup>
import { computed } from 'vue'
import HomogeneousInterfaces from '@/components/HomogeneousInterfaces.vue'
import SchemaFields from '@/components/SchemaFields.vue'

defineOptions({ name: 'ServiceInstanceForm' })

const props = defineProps({
  definition: { type: Object, default: null },
  disabled: { type: Boolean, default: false },
  submitted: { type: Boolean, default: false },
})

const fields = defineModel('fields', { type: Object, default: () => ({}) })
const connectionTypeId = defineModel('connectionTypeId')
const endpoints = defineModel('endpoints', { type: Array, default: () => [] })

const schema = computed(() => props.definition?.schema ?? [])
const spec = computed(() => props.definition?.interfaces ?? { min: 0, max: 0, fields: [] })
const connectionTypes = computed(() => props.definition?.connection_types ?? [])

const firstInterfaceId = computed(
  () => endpoints.value.find((ep) => ep.interface_id)?.interface_id ?? null,
)
const firstDeviceId = computed(() => endpoints.value.find((ep) => ep.device_id)?.device_id ?? null)

</script>

<template>
  <div class="flex flex-col gap-4">
    <SchemaFields
      v-if="schema.length"
      v-model="fields"
      :fields="schema"
      :disabled="disabled"
      :submitted="submitted"
      :interface-id="firstInterfaceId"
      :device-id="firstDeviceId"
    />
    <div v-if="connectionTypes.length">
      <label class="block font-bold mb-2">Connection type</label>
      <div class="flex flex-col gap-2">
        <button
          v-for="ct in connectionTypes"
          :key="ct.id"
          type="button"
          class="flex items-center gap-3 rounded-md ring p-2 text-left"
          :class="connectionTypeId === ct.id ? 'ring-primary' : 'ring-default'"
          :disabled="disabled"
          @click="connectionTypeId = ct.id"
        >
          <img
            v-if="ct.has_image && ct.image_url"
            :src="ct.image_url"
            alt=""
            class="h-12 w-16 object-contain"
          />
          <span>{{ ct.name }}</span>
        </button>
      </div>
      <small v-if="submitted && !connectionTypeId" class="text-red-500">
        Connection type is required.
      </small>
    </div>
    <div>
      <h6 class="m-0 mb-2">Interfaces</h6>
      <HomogeneousInterfaces
        v-model="endpoints"
        :spec="spec"
        :disabled="disabled"
        :submitted="submitted"
      />
    </div>
  </div>
</template>
