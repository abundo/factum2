<script setup>
import PasswordInput from '@/components/PasswordInput.vue'
import SettingsFormPage from '@/components/SettingsFormPage.vue'

const snmpVersionOptions = [
  { label: 'v1', value: 'v1' },
  { label: 'v2c', value: 'v2c' },
  { label: 'v3', value: 'v3' },
]

function onDelayedDeleteToggle(settings, val) {
  settings.librenms_delayed_delete_enabled = val
  if (
    val &&
    (!settings.librenms_delayed_delete_days || settings.librenms_delayed_delete_days < 1)
  ) {
    settings.librenms_delayed_delete_days = 30
  }
}
</script>

<template>
  <SettingsFormPage v-slot="{ settings }" title="LibreNMS">
    <div class="flex items-center gap-2">
      <USwitch v-model="settings.librenms_enabled" id="librenms_enabled" />
      <label for="librenms_enabled" class="font-bold">Enabled</label>
    </div>
    <div>
      <label for="librenms_api_url" class="block font-bold mb-3">API URL</label>
      <UInput id="librenms_api_url" v-model="settings.librenms_api_url" class="w-full" />
      <p class="text-muted-color mt-1">
        LibreNMS REST API origin, for example
        <span class="font-mono">http://librenms:8000/api/v0</span>.
        <span class="font-mono">/api/v0</span>
        is added if missing. Without it, device create hits the web UI and fails with a CSRF error.
      </p>
    </div>
    <div>
      <label for="librenms_api_token" class="block font-bold mb-3">API token</label>
      <PasswordInput id="librenms_api_token" v-model="settings.librenms_api_token" />
    </div>
    <div>
      <label for="librenms_persistent_devices" class="block font-bold mb-3"
        >Persistent devices</label
      >
      <UTextarea
        id="librenms_persistent_devices"
        v-model="settings.librenms_persistent_devices"
        :rows="4"
        placeholder="One hostname or display name per line; never quarantined or deleted by sync"
        class="w-full"
      />
    </div>
    <div class="flex items-center gap-2">
      <USwitch
        id="librenms_delayed_delete_enabled"
        :model-value="!!settings.librenms_delayed_delete_enabled"
        @update:model-value="onDelayedDeleteToggle(settings, $event)"
      />
      <label for="librenms_delayed_delete_enabled" class="font-bold">Delayed deletion</label>
    </div>
    <p class="text-muted-color -mt-3">
      When enabled, devices that would be removed from LibreNMS are disabled (no polling or alerts)
      and shown as
      <span class="font-mono">(scheduled for deletion YYYY-MM-DD)</span>
      on the display name. They are deleted after the delay below. Queue an earlier delete from Jobs
      → Device deletions.
    </p>
    <div>
      <label for="librenms_delayed_delete_days" class="block font-bold mb-3"
        >Delete after (days)</label
      >
      <UInputNumber
        id="librenms_delayed_delete_days"
        v-model="settings.librenms_delayed_delete_days"
        :min="1"
        :format-options="{ useGrouping: false }"
        class="w-full"
      />
    </div>
    <div>
      <label for="librenms_roles_enabled" class="block font-bold mb-3">Roles enabled</label>
      <UTextarea
        id="librenms_roles_enabled"
        v-model="settings.librenms_roles_enabled"
        :rows="4"
        placeholder="One regex per line, matched against an interface's role to force alerting on"
        class="w-full"
      />
    </div>
    <div>
      <label for="librenms_interfaces_disabled" class="block font-bold mb-3"
        >Interfaces disabled</label
      >
      <UTextarea
        id="librenms_interfaces_disabled"
        v-model="settings.librenms_interfaces_disabled"
        :rows="4"
        placeholder="One regex per line, matched against an interface's name to force alerting off"
        class="w-full"
      />
    </div>
    <div>
      <label for="librenms_snmp_version" class="block font-bold mb-3">SNMP version</label>
      <USelect
        id="librenms_snmp_version"
        v-model="settings.librenms_snmp_version"
        :items="snmpVersionOptions"
        class="w-full"
      />
    </div>
    <div>
      <label for="librenms_snmp_communities" class="block font-bold mb-3">SNMP communities</label>
      <UTextarea
        id="librenms_snmp_communities"
        v-model="settings.librenms_snmp_communities"
        :rows="4"
        placeholder="One community per line, tried in order when creating a device"
        class="w-full"
      />
    </div>
  </SettingsFormPage>
</template>
