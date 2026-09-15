<script setup>
import { ref } from 'vue'
import { useSettings } from '@/composables/useSettings'
import GoTemplateField from '@/components/GoTemplateField.vue'
import PasswordInput from '@/components/PasswordInput.vue'
import {
  icingaDefaultNotificationSchema,
  icingaDependencyTemplateSchema,
  icingaHostTemplateSchema,
  icingaUserTemplateSchema,
} from '@/utils/goTemplateSchemas'

const { settings, loading, saving, forbidden, loadError, save } = useSettings()

function fillCertsDefaults() {
  if (!settings.certs_lego_yaml) settings.certs_lego_yaml = '/var/lib/lego/.lego.yaml'
  if (!settings.certs_env_file) settings.certs_env_file = '/var/lib/lego/.env'
  if (!settings.certs_lego_bin) settings.certs_lego_bin = 'lego'
  if (!settings.certs_lego_storage) settings.certs_lego_storage = '/var/lib/lego/storage'
  if (!settings.certs_default_key_type) settings.certs_default_key_type = 'EC256'
}

function onCertsToggle(val) {
  settings.certs_enabled = val
  if (val) fillCertsDefaults()
}

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

const destinationTab = ref('dns')
const destinationTabItems = [
  { label: 'DNS', value: 'dns', slot: 'dns' },
  { label: 'DHCP', value: 'dhcp', slot: 'dhcp' },
  { label: 'Icinga', value: 'icinga', slot: 'icinga' },
  { label: 'LibreNMS', value: 'librenms', slot: 'librenms' },
  { label: 'Oxidized', value: 'oxidized', slot: 'oxidized' },
  { label: 'Prometheus', value: 'prometheus', slot: 'prometheus' },
  { label: 'Certificates', value: 'certs', slot: 'certs' },
]

const certKeyTypeItems = [
  { label: 'EC256', value: 'EC256' },
  { label: 'EC384', value: 'EC384' },
  { label: 'RSA2048', value: 'RSA2048' },
  { label: 'RSA4096', value: 'RSA4096' },
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
      <UTabs v-model="destinationTab" :items="destinationTabItems">
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
            <div class="font-semibold">dnsmgr2 config file</div>
            <small class="text-muted-color -mt-4"
              >When the DNS zone editor is on, factum2-dns writes a dnsmgr2.yaml here as well as the
              records file above. Leave blank to keep a locally maintained config.</small
            >
            <div>
              <label for="dns_config_file" class="block font-bold mb-3">Config file</label>
              <UInput
                id="dns_config_file"
                v-model="settings.dns_config_file"
                placeholder="/etc/dnsmgr2/dnsmgr2.yaml"
                class="w-full"
              />
            </div>
            <div>
              <label for="dns_db_file" class="block font-bold mb-3">SQLite serial DB</label>
              <UInput
                id="dns_db_file"
                v-model="settings.dns_db_file"
                placeholder="/var/lib/dnsmgr2/dnsmgr2.sqlite"
                class="w-full"
              />
            </div>
            <div>
              <label for="dns_host_template" class="block font-bold mb-3">Host template name</label>
              <UInput
                id="dns_host_template"
                v-model="settings.dns_host_template"
                placeholder="isc_bind"
                class="w-full"
              />
            </div>
            <div>
              <label for="dns_bind_config_dir" class="block font-bold mb-3">BIND config dir</label>
              <UInput
                id="dns_bind_config_dir"
                v-model="settings.dns_bind_config_dir"
                placeholder="/etc/bind"
                class="w-full"
              />
            </div>
            <div>
              <label for="dns_bind_include_file" class="block font-bold mb-3">Include file</label>
              <UInput
                id="dns_bind_include_file"
                v-model="settings.dns_bind_include_file"
                placeholder="named.conf.dnsmgr2"
                class="w-full"
              />
            </div>
            <div>
              <label for="dns_bind_zones_dir" class="block font-bold mb-3">Zones dir</label>
              <UInput
                id="dns_bind_zones_dir"
                v-model="settings.dns_bind_zones_dir"
                placeholder="/var/lib/bind"
                class="w-full"
              />
            </div>
            <div>
              <label for="dns_bind_tmp_dir" class="block font-bold mb-3">Temp dir</label>
              <UInput
                id="dns_bind_tmp_dir"
                v-model="settings.dns_bind_tmp_dir"
                placeholder="/var/lib/dnsmgr2"
                class="w-full"
              />
            </div>
            <div>
              <label for="dns_bind_cmd_reload_zone" class="block font-bold mb-3"
                >Reload zone command</label
              >
              <UInput
                id="dns_bind_cmd_reload_zone"
                v-model="settings.dns_bind_cmd_reload_zone"
                placeholder="sudo rndc reload {zone}"
                class="w-full"
              />
            </div>
            <div>
              <label for="dns_bind_cmd_reload_all" class="block font-bold mb-3"
                >Reload all command</label
              >
              <UInput
                id="dns_bind_cmd_reload_all"
                v-model="settings.dns_bind_cmd_reload_all"
                placeholder="sudo rndc reload"
                class="w-full"
              />
            </div>
            <div>
              <label for="dns_bind_cmd_restart" class="block font-bold mb-3">Restart command</label>
              <UInput
                id="dns_bind_cmd_restart"
                v-model="settings.dns_bind_cmd_restart"
                placeholder="systemctl restart named.service"
                class="w-full"
              />
            </div>
          </div>
        </template>

        <template #dhcp>
          <div class="flex flex-col gap-6 py-4">
            <div class="flex items-center gap-2">
              <USwitch
                :model-value="!!settings.dhcp_enabled"
                id="dhcp_enabled"
                @update:model-value="settings.dhcp_enabled = $event"
              />
              <label for="dhcp_enabled" class="font-bold">Enabled</label>
            </div>
            <small class="text-muted-color -mt-4"
              >Per-prefix DHCP in IPAM (range, gateway, DNS servers) and a MAC column on DNS zone
              records for static reservations. Off by default. Turning this off hides the UI; it
              does not delete stored DHCP data. Empty Kea fields fall back to the Ubuntu layout from
              dnsmgr2's example config.</small
            >
            <div v-if="settings.dhcp_enabled">
              <label for="dhcp_dns_servers" class="block font-bold mb-3">Default DNS servers</label>
              <UTextarea
                id="dhcp_dns_servers"
                v-model="settings.dhcp_dns_servers"
                :rows="3"
                placeholder="One IP address per line"
                class="w-full"
              />
              <small class="text-muted-color"
                >Offered to DHCP clients unless a prefix overrides them. Domain name is the default
                domain on Settings → Factum.</small
              >
            </div>
            <div>
              <label for="dhcp_host_template" class="block font-bold mb-3"
                >Host template name</label
              >
              <UInput
                id="dhcp_host_template"
                v-model="settings.dhcp_host_template"
                placeholder="isc_kea"
                class="w-full"
              />
            </div>
            <div>
              <label for="dhcp_kea4_config_dir" class="block font-bold mb-3"
                >DHCPv4 config dir</label
              >
              <UInput
                id="dhcp_kea4_config_dir"
                v-model="settings.dhcp_kea4_config_dir"
                placeholder="/etc/kea"
                class="w-full"
              />
            </div>
            <div>
              <label for="dhcp_kea4_include_file" class="block font-bold mb-3"
                >DHCPv4 include file</label
              >
              <UInput
                id="dhcp_kea4_include_file"
                v-model="settings.dhcp_kea4_include_file"
                placeholder="kea-dhcp4.dnsmgr2.json"
                class="w-full"
              />
              <small class="text-muted-color"
                >JSON array of subnets written by dnsmgr2. Include it from the main Kea config as
                <code>"subnet4": &lt;?include "/etc/kea/kea-dhcp4.dnsmgr2.json"?&gt;</code> — not
                the main config file itself.</small
              >
            </div>
            <div>
              <label for="dhcp_kea4_cmd_restart" class="block font-bold mb-3"
                >DHCPv4 restart command</label
              >
              <UInput
                id="dhcp_kea4_cmd_restart"
                v-model="settings.dhcp_kea4_cmd_restart"
                placeholder="systemctl restart kea-dhcp4-server"
                class="w-full"
              />
            </div>
            <div>
              <label for="dhcp_kea6_config_dir" class="block font-bold mb-3"
                >DHCPv6 config dir</label
              >
              <UInput
                id="dhcp_kea6_config_dir"
                v-model="settings.dhcp_kea6_config_dir"
                placeholder="/etc/kea"
                class="w-full"
              />
            </div>
            <div>
              <label for="dhcp_kea6_include_file" class="block font-bold mb-3"
                >DHCPv6 include file</label
              >
              <UInput
                id="dhcp_kea6_include_file"
                v-model="settings.dhcp_kea6_include_file"
                placeholder="kea-dhcp6.dnsmgr2.json"
                class="w-full"
              />
              <small class="text-muted-color"
                >Same include pattern as DHCPv4, with
                <code>"subnet6": &lt;?include "/etc/kea/kea-dhcp6.dnsmgr2.json"?&gt;</code>.</small
              >
            </div>
            <div>
              <label for="dhcp_kea6_cmd_restart" class="block font-bold mb-3"
                >DHCPv6 restart command</label
              >
              <UInput
                id="dhcp_kea6_cmd_restart"
                v-model="settings.dhcp_kea6_cmd_restart"
                placeholder="systemctl restart kea-dhcp6-server"
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
            <GoTemplateField
              id="icinga_default_notification"
              v-model="settings.icinga_default_notification"
              label="Default notification"
              :rows="4"
              placeholder="Go template; inserted into the host object when a device has no alarm destination"
              :schema="icingaDefaultNotificationSchema"
            />
            <GoTemplateField
              id="icinga_host_template"
              v-model="settings.icinga_host_template"
              label="Host template"
              :rows="6"
              placeholder="Go template, executed with .Device and .Options"
              :schema="icingaHostTemplateSchema"
            />
            <GoTemplateField
              id="icinga_dependency_template"
              v-model="settings.icinga_dependency_template"
              label="Dependency template"
              :rows="6"
              :schema="icingaDependencyTemplateSchema"
            />
            <GoTemplateField
              id="icinga_user_template"
              v-model="settings.icinga_user_template"
              label="User template"
              :rows="6"
              placeholder="Go template, executed with .Username, .DisplayName and .Email"
              :schema="icingaUserTemplateSchema"
            />
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
              <p class="text-muted-color mt-1">
                LibreNMS REST API origin, for example
                <span class="font-mono">http://librenms:8000/api/v0</span>.
                <span class="font-mono">/api/v0</span>
                is added if missing. Without it, device create hits the web UI and fails with a CSRF
                error.
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
                <span class="font-mono">name: 0</span>, <span class="font-mono">ip: 1</span>,
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

        <template #certs>
          <div class="flex flex-col gap-6 py-4">
            <div class="flex items-center gap-2">
              <USwitch
                :model-value="!!settings.certs_enabled"
                id="certs_enabled"
                @update:model-value="onCertsToggle"
              />
              <label for="certs_enabled" class="font-bold">Enabled</label>
            </div>
            <small class="text-muted-color -mt-4"
              >Certificate table, ACME accounts, and DNS-01/RFC2136 challenges. Sync writes
              <span class="font-mono">.lego.yaml</span> and <span class="font-mono">.env</span>,
              then runs lego. Certificate distribution and service restarts are out of scope.</small
            >
            <div>
              <label for="certs_lego_yaml" class="block font-bold mb-3">lego YAML path</label>
              <UInput
                id="certs_lego_yaml"
                v-model="settings.certs_lego_yaml"
                placeholder="/var/lib/lego/.lego.yaml"
                class="w-full"
              />
            </div>
            <div>
              <label for="certs_env_file" class="block font-bold mb-3">.env path</label>
              <UInput
                id="certs_env_file"
                v-model="settings.certs_env_file"
                placeholder="/var/lib/lego/.env"
                class="w-full"
              />
            </div>
            <div>
              <label for="certs_lego_bin" class="block font-bold mb-3">lego binary</label>
              <UInput
                id="certs_lego_bin"
                v-model="settings.certs_lego_bin"
                placeholder="lego"
                class="w-full"
              />
            </div>
            <div>
              <label for="certs_lego_storage" class="block font-bold mb-3">lego storage dir</label>
              <UInput
                id="certs_lego_storage"
                v-model="settings.certs_lego_storage"
                placeholder="/var/lib/lego"
                class="w-full"
              />
            </div>
            <div>
              <label for="certs_default_key_type" class="block font-bold mb-3"
                >Default certificate key type</label
              >
              <USelect
                id="certs_default_key_type"
                v-model="settings.certs_default_key_type"
                :items="certKeyTypeItems"
                class="w-full"
              />
            </div>
            <div class="flex items-center gap-2">
              <USwitch
                :model-value="!!settings.certs_default_enable_common_name"
                id="certs_default_enable_common_name"
                @update:model-value="settings.certs_default_enable_common_name = $event"
              />
              <label for="certs_default_enable_common_name" class="font-bold"
                >Default enable Common Name</label
              >
            </div>
            <small class="text-muted-color -mt-4"
              >CN is deprecated in ACME. Per-certificate overrides live on each certificate
              row.</small
            >
          </div>
        </template>
      </UTabs>
    </template>
  </div>
</template>
