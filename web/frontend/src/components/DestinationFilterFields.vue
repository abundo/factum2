<script setup>
const props = defineProps({
  settings: { type: Object, required: true },
  prefix: { type: String, required: true },
})

function field(name) {
  return `${props.prefix}_${name}`
}
</script>

<template>
  <div>
    <label :for="field('ignore_devices')" class="block font-bold mb-3">Ignore devices</label>
    <UTextarea
      :id="field('ignore_devices')"
      v-model="settings[field('ignore_devices')]"
      :rows="4"
      placeholder="One device name per line"
      class="w-full"
    />
    <small v-if="prefix !== 'common'" class="text-muted-color">Empty uses Destinations → Common.</small>
  </div>
  <div>
    <label :for="field('ignore_models')" class="block font-bold mb-3">Ignore models</label>
    <UTextarea
      :id="field('ignore_models')"
      v-model="settings[field('ignore_models')]"
      :rows="4"
      placeholder="One model per line"
      class="w-full"
    />
    <small v-if="prefix !== 'common'" class="text-muted-color">Empty uses Destinations → Common.</small>
  </div>
  <div>
    <label :for="field('ignore_platforms')" class="block font-bold mb-3">Ignore platforms</label>
    <UTextarea
      :id="field('ignore_platforms')"
      v-model="settings[field('ignore_platforms')]"
      :rows="4"
      placeholder="One platform per line"
      class="w-full"
    />
    <small v-if="prefix !== 'common'" class="text-muted-color">Empty uses Destinations → Common.</small>
  </div>
  <div>
    <label :for="field('ignore_manufacturers')" class="block font-bold mb-3">Ignore manufacturers</label>
    <UTextarea
      :id="field('ignore_manufacturers')"
      v-model="settings[field('ignore_manufacturers')]"
      :rows="4"
      placeholder="One manufacturer per line"
      class="w-full"
    />
    <small v-if="prefix !== 'common'" class="text-muted-color">Empty uses Destinations → Common.</small>
  </div>
  <div>
    <label :for="field('ignore_interfaces')" class="block font-bold mb-3">Ignore interfaces</label>
    <UTextarea
      :id="field('ignore_interfaces')"
      v-model="settings[field('ignore_interfaces')]"
      :rows="4"
      placeholder="One regexp per line"
      class="w-full"
    />
    <small v-if="prefix !== 'common'" class="text-muted-color">Empty uses Destinations → Common.</small>
    <slot name="ignore-interfaces" />
  </div>
  <div>
    <label :for="field('roles_enabled')" class="block font-bold mb-3">Roles enabled</label>
    <UTextarea
      :id="field('roles_enabled')"
      v-model="settings[field('roles_enabled')]"
      :rows="4"
      placeholder="One regex per line"
      class="w-full"
    />
    <small v-if="prefix !== 'common'" class="text-muted-color">Empty uses Destinations → Common.</small>
  </div>
</template>
