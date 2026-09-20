<script setup>
import PasswordInput from '@/components/PasswordInput.vue'
import SettingsFormPage from '@/components/SettingsFormPage.vue'
</script>

<template>
  <SettingsFormPage v-slot="{ settings }" title="Netbox">
    <div class="flex items-center gap-2">
      <USwitch v-model="settings.netbox_enabled" id="netbox_enabled" />
      <label for="netbox_enabled" class="font-bold">Enabled</label>
    </div>
    <div>
      <label for="netbox_api_url" class="block font-bold mb-3">API URL</label>
      <UInput id="netbox_api_url" v-model="settings.netbox_api_url" class="w-full" />
    </div>
    <div>
      <label for="netbox_api_token" class="block font-bold mb-3">API token</label>
      <PasswordInput id="netbox_api_token" v-model="settings.netbox_api_token" />
    </div>
    <div>
      <label for="netbox_webhook_secret" class="block font-bold mb-3">Webhook secret</label>
      <PasswordInput id="netbox_webhook_secret" v-model="settings.netbox_webhook_secret" />
    </div>
    <div class="flex items-center gap-2">
      <USwitch
        v-model="settings.netbox_sync_customers_enabled"
        id="netbox_sync_customers_enabled"
      />
      <label for="netbox_sync_customers_enabled" class="font-bold"
        >Sync customers to Netbox as tenants</label
      >
    </div>
    <div class="flex items-center gap-2">
      <USwitch v-model="settings.netbox_sync_contacts_enabled" id="netbox_sync_contacts_enabled" />
      <label for="netbox_sync_contacts_enabled" class="font-bold">Sync contacts to Netbox</label>
    </div>
  </SettingsFormPage>
</template>
