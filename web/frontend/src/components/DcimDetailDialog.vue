<script setup>
defineOptions({ inheritAttrs: false, name: 'DcimDetailDialog' })

const open = defineModel('open', { type: Boolean })
const tab = defineModel('tab', { type: String })

defineProps({
  tabs: { type: Array, required: true },
  loading: { type: Boolean, default: false },
  error: { type: String, default: null },
  ready: { type: Boolean, default: true },
  dirty: { type: Boolean, default: undefined },
})
</script>

<template>
  <FormModal
    v-bind="$attrs"
    v-model:open="open"
    :dirty="dirty"
    :ui="{
      content: 'w-[95vw] h-[90vh] sm:max-w-none flex flex-col',
      header: 'min-w-0',
      title: 'min-w-0 flex-1',
      body: 'flex-1 min-h-0 overflow-hidden',
    }"
  >
    <template v-if="$slots.title" #title="slotProps">
      <slot name="title" v-bind="slotProps ?? {}" />
    </template>
    <template #body>
      <div v-if="loading" class="flex justify-center p-4">
        <UIcon name="i-lucide-loader-2" class="size-8 animate-spin" />
      </div>
      <UAlert v-else-if="error" color="error" variant="subtle" :title="error" />
      <div v-else-if="ready" class="flex flex-col h-full min-h-0">
        <UTabs
          v-model="tab"
          :items="tabs"
          class="min-h-0 flex-1"
          :ui="{
            list: 'w-full',
            content: 'min-h-0 flex-1 overflow-auto rounded-md border border-default p-3 mt-2',
          }"
        >
          <template #overview>
            <slot name="overview" />
          </template>
          <template #interfaces>
            <slot name="interfaces" />
          </template>
          <template #vlans>
            <slot name="vlans" />
          </template>
          <template #oxidized>
            <slot name="oxidized" />
          </template>
        </UTabs>
      </div>
    </template>
    <template v-if="$slots.footer" #footer="slotProps">
      <slot name="footer" v-bind="slotProps ?? {}" />
    </template>
  </FormModal>
</template>
