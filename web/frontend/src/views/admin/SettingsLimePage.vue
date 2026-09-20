<script setup>
import PasswordInput from '@/components/PasswordInput.vue'
import SettingsFormPage from '@/components/SettingsFormPage.vue'
</script>

<template>
  <SettingsFormPage v-slot="{ settings }" title="Lime">
    <div class="flex items-center gap-2">
      <USwitch v-model="settings.lime_enabled" id="lime_enabled" />
      <label for="lime_enabled" class="font-bold">Enabled</label>
    </div>
    <div>
      <label for="lime_api_url" class="block font-bold mb-3">API URL</label>
      <UInput id="lime_api_url" v-model="settings.lime_api_url" class="w-full" />
    </div>
    <div>
      <label for="lime_api_token" class="block font-bold mb-3">API token</label>
      <PasswordInput id="lime_api_token" v-model="settings.lime_api_token" />
    </div>
  </SettingsFormPage>
</template>
