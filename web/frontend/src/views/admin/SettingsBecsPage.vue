<script setup>
import PasswordInput from '@/components/PasswordInput.vue'
import SettingsFormPage from '@/components/SettingsFormPage.vue'
</script>

<template>
  <SettingsFormPage v-slot="{ settings }" title="BECS">
    <div class="flex items-center gap-2">
      <USwitch v-model="settings.becs_enabled" id="becs_enabled" />
      <label for="becs_enabled" class="font-bold">Enabled</label>
    </div>
    <div>
      <label for="becs_eapi_url" class="block font-bold mb-3">EAPI URL</label>
      <UInput id="becs_eapi_url" v-model="settings.becs_eapi_url" class="w-full" />
    </div>
    <div>
      <label for="becs_eapi_user" class="block font-bold mb-3">EAPI User</label>
      <UInput id="becs_eapi_user" v-model="settings.becs_eapi_user" class="w-full" />
    </div>
    <div>
      <label for="becs_eapi_pass" class="block font-bold mb-3">EAPI Password</label>
      <PasswordInput id="becs_eapi_pass" v-model="settings.becs_eapi_pass" />
    </div>
    <div>
      <label for="becs_eapi_oid" class="block font-bold mb-3">EAPI OID</label>
      <UInputNumber
        id="becs_eapi_oid"
        v-model="settings.becs_eapi_oid"
        :format-options="{ useGrouping: false }"
        class="w-full"
      />
    </div>
  </SettingsFormPage>
</template>
