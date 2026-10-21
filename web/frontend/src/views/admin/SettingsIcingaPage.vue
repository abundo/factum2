<script setup>
import GoTemplateField from '@/components/GoTemplateField.vue'
import PasswordInput from '@/components/PasswordInput.vue'
import SettingsFormPage from '@/components/SettingsFormPage.vue'
import {
  icingaCertTemplateExample,
  icingaCertTemplateSchema,
  icingaDefaultNotificationSchema,
  icingaDependencyTemplateSchema,
  icingaHostTemplateSchema,
  icingaUserTemplateSchema,
} from '@/utils/goTemplateSchemas'
</script>

<template>
  <SettingsFormPage v-slot="{ settings }" title="Icinga">
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
      <label for="icinga_ignore_devices" class="block font-bold mb-3">Ignore devices</label>
      <UTextarea
        id="icinga_ignore_devices"
        v-model="settings.icinga_ignore_devices"
        :rows="4"
        placeholder="One device name per line"
        class="w-full"
      />
    </div>
    <div class="flex items-center gap-2">
      <USwitch v-model="settings.icinga_monitor_interfaces" id="icinga_monitor_interfaces" />
      <label for="icinga_monitor_interfaces" class="font-bold">Monitor interfaces</label>
    </div>
    <p class="text-muted-color -mt-4">
      When on, sync writes one passive Icinga service per interface for devices that have Interface
      alarms set. <span class="font-mono">factum2-icinga poll --interval 1m</span> reads oper status
      and traffic counters with one SNMPv2c session per device and submits the results. One session
      per device is what keeps up with about 40000 interfaces. Set
      <span class="font-mono">icinga_command_pipe</span> in the worker YAML to the Icinga external
      command pipe so poll can submit results in bulk.
    </p>
    <div>
      <label for="icinga_snmp_communities" class="block font-bold mb-3">SNMP communities</label>
      <UTextarea
        id="icinga_snmp_communities"
        v-model="settings.icinga_snmp_communities"
        :rows="3"
        placeholder="One SNMPv2c community per line, tried in order"
        class="w-full"
      />
    </div>
    <div>
      <label for="icinga_interfaces_file" class="block font-bold mb-3">Interfaces file</label>
      <UInput
        id="icinga_interfaces_file"
        v-model="settings.icinga_interfaces_file"
        placeholder="icinga_interfaces_file"
        class="w-full"
      />
      <small class="text-muted-color"
        >worker.paths key. The file path is set only in each worker's factum2-worker.yaml. Leave
        blank to use icinga_interfaces_file. Include that file next to the hosts file. Run
        factum2-icinga poll on the Icinga host; sync only writes the service objects.</small
      >
    </div>
    <GoTemplateField
      id="icinga_default_notification"
      v-model="settings.icinga_default_notification"
      label="Default notification"
      :rows="4"
      placeholder="Jet template; inserted into the host object when a device has no alarm destination"
      :schema="icingaDefaultNotificationSchema"
    />
    <div>
      <label for="icinga_hosts_file" class="block font-bold mb-3">Hosts file</label>
      <UInput
        id="icinga_hosts_file"
        v-model="settings.icinga_hosts_file"
        placeholder="icinga_hosts_file"
        class="w-full"
      />
      <small class="text-muted-color"
        >worker.paths key. The file path is set only in each worker's factum2-worker.yaml. Leave
        blank to use icinga_hosts_file.</small
      >
    </div>
    <GoTemplateField
      id="icinga_host_template"
      v-model="settings.icinga_host_template"
      label="Host template"
      :rows="6"
      placeholder="Jet template, executed with .Device and .Options"
      :schema="icingaHostTemplateSchema"
    />
    <GoTemplateField
      id="icinga_dependency_template"
      v-model="settings.icinga_dependency_template"
      label="Dependency template"
      :rows="6"
      :schema="icingaDependencyTemplateSchema"
    />
    <div>
      <label for="icinga_users_file" class="block font-bold mb-3">Users file</label>
      <UInput
        id="icinga_users_file"
        v-model="settings.icinga_users_file"
        placeholder="icinga_users_file"
        class="w-full"
      />
      <small class="text-muted-color"
        >worker.paths key, for example icinga_users_file. The file path is set only in each worker's
        factum2-worker.yaml. Leave blank to use icinga_users_file.</small
      >
    </div>
    <GoTemplateField
      id="icinga_user_template"
      v-model="settings.icinga_user_template"
      label="User template"
      :rows="6"
      placeholder="Jet template, executed with .Username, .DisplayName and .Email"
      :schema="icingaUserTemplateSchema"
    />
    <div>
      <label for="icinga_certs_file" class="block font-bold mb-3">Certificates file</label>
      <UInput
        id="icinga_certs_file"
        v-model="settings.icinga_certs_file"
        placeholder="icinga_certs_file"
        class="w-full"
      />
      <small class="text-muted-color"
        >worker.paths key, for example icinga_certs_file. factum2-icinga writes the Icinga 2 conf
        for HTTPS certificate checks at the path that key names on the worker. Include that path
        next to the hosts and users files. Leave blank to skip writing when icinga_certs_file is not
        set on the worker.</small
      >
    </div>
    <GoTemplateField
      id="icinga_cert_template"
      v-model="settings.icinga_cert_template"
      label="Certificate template"
      :rows="12"
      :placeholder="icingaCertTemplateExample"
      :schema="icingaCertTemplateSchema"
    />
  </SettingsFormPage>
</template>
