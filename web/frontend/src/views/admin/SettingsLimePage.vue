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
    <div>
      <label for="lime_report_to" class="block font-bold mb-3">Change report</label>
      <UTextarea
        id="lime_report_to"
        v-model="settings.lime_report_to"
        :rows="3"
        placeholder="One address per line"
        class="w-full"
      />
      <small class="text-muted-color"
        >Emailed when a Lime sync adds, updates, or removes customers, services, or contacts.
        Nothing is sent when the sync finds no changes. Uses the SMTP settings under Factum →
        Email.</small
      >
    </div>
  </SettingsFormPage>
</template>
