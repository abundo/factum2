<script setup>
import { useSettings } from '@/composables/useSettings'
import PasswordInput from '@/components/PasswordInput.vue'

const { settings, loading, saving, forbidden, loadError, save } = useSettings()

function onDelayedDeleteToggle(val) {
  settings.librenms_delayed_delete_enabled = val
  if (
    val &&
    (!settings.librenms_delayed_delete_days || settings.librenms_delayed_delete_days < 1)
  ) {
    settings.librenms_delayed_delete_days = 30
  }
}

const snmpVersionOptions = [
  { label: 'v1', value: 'v1' },
  { label: 'v2c', value: 'v2c' },
  { label: 'v3', value: 'v3' },
]

const destinationTabItems = [
  { label: 'DNS', value: 'dns', slot: 'dns' },
  { label: 'Icinga', value: 'icinga', slot: 'icinga' },
  { label: 'LibreNMS', value: 'librenms', slot: 'librenms' },
  { label: 'Oxidized', value: 'oxidized', slot: 'oxidized' },
  { label: 'Prometheus', value: 'prometheus', slot: 'prometheus' },
]
</script>

<template>
  <div v-if="forbidden" class="card">
    <UAlert
      color="error"
      variant="subtle"
      title="You need administrator permissions to view settings."
    />
  </div>
  <div v-else-if="loadError" class="card">
    <UAlert color="error" variant="subtle" title="Failed to load settings." />
  </div>
  <div v-else class="card">
    <div class="flex justify-end mb-6">
      <UButton
        label="Save"
        icon="i-lucide-check"
        :loading="saving"
        :disabled="loading"
        @click="save"
      />
    </div>

    <div v-if="loading" class="flex justify-center p-4">
      <UIcon name="i-lucide-loader-2" class="size-8 animate-spin" />
    </div>

    <template v-else>
      <div class="font-semibold text-lg mb-3">Destinations</div>
      <UTabs :items="destinationTabItems" default-value="dns">
        <template #dns>
          <div class="flex flex-col gap-6 py-4">
            <div class="flex items-center gap-2">
              <USwitch v-model="settings.dns_enabled" id="dns_enabled" />
              <label for="dns_enabled" class="font-bold">Enabled</label>
            </div>
            <div>
              <label for="dns_dest_file" class="block font-bold mb-3">Destination file</label>
              <UInput id="dns_dest_file" v-model="settings.dns_dest_file" class="w-full" />
            </div>
            <div>
              <label for="dns_ignore_models" class="block font-bold mb-3">Ignore models</label>
              <UTextarea
                id="dns_ignore_models"
                v-model="settings.dns_ignore_models"
                :rows="4"
                placeholder="One model per line"
                class="w-full"
              />
            </div>
            <div>
              <label for="dns_ignore_platforms" class="block font-bold mb-3"
                >Ignore platforms</label
              >
              <UTextarea
                id="dns_ignore_platforms"
                v-model="settings.dns_ignore_platforms"
                :rows="4"
                placeholder="One platform per line"
                class="w-full"
              />
            </div>
          </div>
        </template>

        <template #icinga>
          <div class="flex flex-col gap-6 py-4">
            <div class="flex items-center gap-2">
              <USwitch v-model="settings.icinga_enabled" id="icinga_enabled" />
              <label for="icinga_enabled" class="font-bold">Enabled</label>
            </div>
            <div>
              <label for="icinga_api_url" class="block font-bold mb-3">API URL</label>
              <UInput id="icinga_api_url" v-model="settings.icinga_api_url" class="w-full" />
            </div>
            <div>
              <label for="icinga_api_user" class="block font-bold mb-3">API User</label>
              <UInput id="icinga_api_user" v-model="settings.icinga_api_user" class="w-full" />
            </div>
            <div>
              <label for="icinga_api_pass" class="block font-bold mb-3">API Password</label>
              <PasswordInput id="icinga_api_pass" v-model="settings.icinga_api_pass" />
            </div>
            <div>
              <label for="icinga_hosts_file" class="block font-bold mb-3">Hosts file</label>
              <UInput id="icinga_hosts_file" v-model="settings.icinga_hosts_file" class="w-full" />
            </div>
            <div>
              <label for="icinga_users_file" class="block font-bold mb-3">Users file</label>
              <UInput id="icinga_users_file" v-model="settings.icinga_users_file" class="w-full" />
            </div>
            <div>
              <label for="icinga_ignore_devices" class="block font-bold mb-3">Ignore devices</label>
              <UTextarea
                id="icinga_ignore_devices"
                v-model="settings.icinga_ignore_devices"
                :rows="4"
                placeholder="One device name per line"
                class="w-full"
              />
            </div>
            <div>
              <label for="icinga_default_notification" class="block font-bold mb-3"
                >Default notification</label
              >
              <UTextarea
                id="icinga_default_notification"
                v-model="settings.icinga_default_notification"
                :rows="2"
                placeholder="Config line(s) applied when a device has no alarm destination set"
                class="w-full"
              />
            </div>
            <div>
              <label for="icinga_host_template" class="block font-bold mb-3">Host template</label>
              <UTextarea
                id="icinga_host_template"
                v-model="settings.icinga_host_template"
                :rows="4"
                placeholder="Go template, executed with .Device and .Options"
                class="w-full"
              />
            </div>
            <div>
              <label for="icinga_dependency_template" class="block font-bold mb-3"
                >Dependency template</label
              >
              <UTextarea
                id="icinga_dependency_template"
                v-model="settings.icinga_dependency_template"
                :rows="4"
                class="w-full"
              />
            </div>
            <div>
              <label for="icinga_user_template" class="block font-bold mb-3">User template</label>
              <UTextarea
                id="icinga_user_template"
                v-model="settings.icinga_user_template"
                :rows="4"
                placeholder="Go template, executed with .Username, .DisplayName and .Email"
                class="w-full"
              />
            </div>
          </div>
        </template>

        <template #librenms>
          <div class="flex flex-col gap-6 py-4">
            <div class="flex items-center gap-2">
              <USwitch v-model="settings.librenms_enabled" id="librenms_enabled" />
              <label for="librenms_enabled" class="font-bold">Enabled</label>
            </div>
            <div>
              <label for="librenms_api_url" class="block font-bold mb-3">API URL</label>
              <UInput id="librenms_api_url" v-model="settings.librenms_api_url" class="w-full" />
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
                @update:model-value="onDelayedDeleteToggle"
              />
              <label for="librenms_delayed_delete_enabled" class="font-bold"
                >Delayed deletion</label
              >
            </div>
            <p class="text-muted-color -mt-3">
              When enabled, devices that would be removed from LibreNMS are disabled (no polling or
              alerts) and shown as
              <span class="font-mono">(scheduled for deletion YYYY-MM-DD)</span>
              on the display name. They are deleted after the delay below. Queue an earlier delete
              from Jobs → Device deletions.
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
              <label for="librenms_snmp_communities" class="block font-bold mb-3"
                >SNMP communities</label
              >
              <UTextarea
                id="librenms_snmp_communities"
                v-model="settings.librenms_snmp_communities"
                :rows="4"
                placeholder="One community per line, tried in order when creating a device"
                class="w-full"
              />
            </div>
          </div>
        </template>

        <template #oxidized>
          <div class="flex flex-col gap-6 py-4">
            <div class="flex items-center gap-2">
              <USwitch v-model="settings.oxidized_enabled" id="oxidized_enabled" />
              <label for="oxidized_enabled" class="font-bold">Enabled</label>
            </div>
            <div>
              <label for="oxidized_api_url" class="block font-bold mb-3">API URL</label>
              <UInput id="oxidized_api_url" v-model="settings.oxidized_api_url" class="w-full" />
              <p class="text-muted-color mt-1">
                oxidized-web REST API, used by
                <span class="font-mono">factum2-oxidized</span>
                and the Oxidized device browser. The browser runs on this factum-web host, so the
                URL must be reachable from here (not only
                <span class="font-mono">127.0.0.1</span>
                on the Oxidized server).
              </p>
            </div>
            <div>
              <label for="oxidized_api_user" class="block font-bold mb-3">API User</label>
              <UInput id="oxidized_api_user" v-model="settings.oxidized_api_user" class="w-full" />
            </div>
            <div>
              <label for="oxidized_api_pass" class="block font-bold mb-3">API Password</label>
              <PasswordInput id="oxidized_api_pass" v-model="settings.oxidized_api_pass" />
            </div>
            <div>
              <label for="oxidized_dest_file" class="block font-bold mb-3">Destination file</label>
              <UInput
                id="oxidized_dest_file"
                v-model="settings.oxidized_dest_file"
                class="w-full"
              />
              <p class="text-muted-color mt-1">
                Oxidized
                <span class="font-mono">router.db</span>
                written by
                <span class="font-mono">factum2-oxidized</span>
                as
                <span class="font-mono">name:ip:model</span>
                per line (FQDN, primary IPv4, platform). Oxidized's CSV source must map
                <span class="font-mono">name: 0</span>,
                <span class="font-mono">ip: 1</span>,
                <span class="font-mono">model: 2</span>
                — the previous two-column
                <span class="font-mono">name:model</span>
                map would treat the IP as the model.
              </p>
            </div>
            <div>
              <label for="oxidized_ignore_devices" class="block font-bold mb-3"
                >Ignore devices</label
              >
              <UTextarea
                id="oxidized_ignore_devices"
                v-model="settings.oxidized_ignore_devices"
                :rows="4"
                placeholder="One device name per line"
                class="w-full"
              />
            </div>
            <div>
              <label for="oxidized_ignore_manufacturers" class="block font-bold mb-3"
                >Ignore manufacturers</label
              >
              <UTextarea
                id="oxidized_ignore_manufacturers"
                v-model="settings.oxidized_ignore_manufacturers"
                :rows="4"
                placeholder="One manufacturer per line"
                class="w-full"
              />
            </div>
            <div>
              <label for="oxidized_ignore_models" class="block font-bold mb-3">Ignore models</label>
              <UTextarea
                id="oxidized_ignore_models"
                v-model="settings.oxidized_ignore_models"
                :rows="4"
                placeholder="One model per line"
                class="w-full"
              />
            </div>
            <div>
              <label for="oxidized_ignore_platforms" class="block font-bold mb-3"
                >Ignore platforms</label
              >
              <UTextarea
                id="oxidized_ignore_platforms"
                v-model="settings.oxidized_ignore_platforms"
                :rows="4"
                placeholder="One platform per line"
                class="w-full"
              />
            </div>
          </div>
        </template>

        <template #prometheus>
          <div class="flex flex-col gap-6 py-4">
            <div class="flex items-center gap-2">
              <USwitch v-model="settings.prometheus_enabled" id="prometheus_enabled" />
              <label for="prometheus_enabled" class="font-bold">Enabled</label>
            </div>
            <p class="text-muted-color -mt-3">
              Writes a Prometheus file_sd JSON of SNMP targets for snmp_exporter. Devices with the
              NetBox custom field
              <span class="font-mono">monitor_grafana</span>
              (and a primary IPv4) are included. The Prometheus scrape job that points at
              snmp_exporter is not generated — only the target list.
            </p>
            <div>
              <label for="prometheus_dest_file" class="block font-bold mb-3"
                >Destination file</label
              >
              <UInput
                id="prometheus_dest_file"
                v-model="settings.prometheus_dest_file"
                placeholder="/etc/prometheus/snmp_targets.json"
                class="w-full"
              />
            </div>
            <div>
              <label for="prometheus_reload_url" class="block font-bold mb-3">Reload URL</label>
              <UInput
                id="prometheus_reload_url"
                v-model="settings.prometheus_reload_url"
                placeholder="http://127.0.0.1:9090/-/reload"
                class="w-full"
              />
              <p class="text-muted-color mt-1">
                Optional. POSTed when the target file changes. Requires Prometheus
                <span class="font-mono">--web.enable-lifecycle</span>. Leave empty to rely on
                file_sd's refresh interval.
              </p>
            </div>
            <div>
              <label for="prometheus_module" class="block font-bold mb-3"
                >snmp_exporter module</label
              >
              <UInput
                id="prometheus_module"
                v-model="settings.prometheus_module"
                placeholder="if_mib"
                class="w-full"
              />
            </div>
            <div>
              <label for="prometheus_auth" class="block font-bold mb-3">snmp_exporter auth</label>
              <UInput
                id="prometheus_auth"
                v-model="settings.prometheus_auth"
                placeholder="public_v2"
                class="w-full"
              />
              <p class="text-muted-color mt-1">
                Name of an <span class="font-mono">auths:</span> entry in snmp_exporter's
                <span class="font-mono">snmp.yml</span>, not the community string itself.
              </p>
            </div>
            <div>
              <label for="prometheus_ignore_devices" class="block font-bold mb-3"
                >Ignore devices</label
              >
              <UTextarea
                id="prometheus_ignore_devices"
                v-model="settings.prometheus_ignore_devices"
                :rows="4"
                placeholder="One device name per line"
                class="w-full"
              />
            </div>
            <div>
              <label for="prometheus_ignore_manufacturers" class="block font-bold mb-3"
                >Ignore manufacturers</label
              >
              <UTextarea
                id="prometheus_ignore_manufacturers"
                v-model="settings.prometheus_ignore_manufacturers"
                :rows="4"
                placeholder="One manufacturer per line"
                class="w-full"
              />
            </div>
            <div>
              <label for="prometheus_ignore_models" class="block font-bold mb-3"
                >Ignore models</label
              >
              <UTextarea
                id="prometheus_ignore_models"
                v-model="settings.prometheus_ignore_models"
                :rows="4"
                placeholder="One model per line"
                class="w-full"
              />
            </div>
            <div>
              <label for="prometheus_ignore_platforms" class="block font-bold mb-3"
                >Ignore platforms</label
              >
              <UTextarea
                id="prometheus_ignore_platforms"
                v-model="settings.prometheus_ignore_platforms"
                :rows="4"
                placeholder="One platform per line"
                class="w-full"
              />
            </div>
          </div>
        </template>
      </UTabs>
    </template>
  </div>
</template>
