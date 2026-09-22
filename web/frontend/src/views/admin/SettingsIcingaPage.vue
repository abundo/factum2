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
      <UInput id="icinga_hosts_file" v-model="settings.icinga_hosts_file" class="w-full" />
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
      <UInput id="icinga_users_file" v-model="settings.icinga_users_file" class="w-full" />
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
      <UInput id="icinga_certs_file" v-model="settings.icinga_certs_file" class="w-full" />
      <small class="text-muted-color"
        >Icinga 2 conf written by factum2-icinga for HTTPS certificate checks. Include it next to
        the hosts and users files. Leave blank to skip writing.</small
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
