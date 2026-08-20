<script setup>
import { useSettings } from '@/composables/useSettings'
import PasswordInput from '@/components/PasswordInput.vue'

const { settings, loading, saving, forbidden, loadError, save } = useSettings()

const sourceTabItems = [
  { label: 'BECS', value: 'becs', slot: 'becs' },
  { label: 'Netbox', value: 'netbox', slot: 'netbox' },
  { label: 'Lime', value: 'lime', slot: 'lime' },
]
</script>

<template>
  <div v-if="forbidden" class="card">
    <UAlert color="error" variant="subtle" title="You need administrator permissions to view settings." />
  </div>
  <div v-else-if="loadError" class="card">
    <UAlert color="error" variant="subtle" title="Failed to load settings." />
  </div>
  <div v-else class="card">
    <div class="flex justify-end mb-6">
      <UButton label="Save" icon="i-lucide-check" :loading="saving" :disabled="loading" @click="save" />
    </div>

    <div v-if="loading" class="flex justify-center p-4">
      <UIcon name="i-lucide-loader-2" class="size-8 animate-spin" />
    </div>

    <template v-else>
      <div class="font-semibold text-lg mb-3">Sources</div>
      <UTabs :items="sourceTabItems" default-value="becs">
        <template #becs>
          <div class="flex flex-col gap-6 py-4">
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
          </div>
        </template>

        <template #netbox>
          <div class="flex flex-col gap-6 py-4">
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
          </div>
        </template>

        <template #lime>
          <div class="flex flex-col gap-6 py-4">
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
          </div>
        </template>
      </UTabs>
    </template>
  </div>
</template>
