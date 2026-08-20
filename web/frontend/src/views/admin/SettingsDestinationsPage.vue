<script setup>
import { useToast } from '@nuxt/ui/composables'
import { ref } from 'vue'
import { useSettings } from '@/composables/useSettings'
import PasswordInput from '@/components/PasswordInput.vue'
import { sendTestEmail } from '@/api/settings'

const { settings, loading, saving, forbidden, loadError, save } = useSettings()

const toast = useToast()
const testEmailTo = ref('')
const testingEmail = ref(false)

function testEmail() {
  testingEmail.value = true
  sendTestEmail({
    to: testEmailTo.value,
    smtp_host: settings.smtp_host,
    smtp_port: settings.smtp_port,
    smtp_user: settings.smtp_user,
    smtp_pass: settings.smtp_pass,
    smtp_tls_mode: settings.smtp_tls_mode,
    email_sender: settings.email_sender,
  })
    .then((data) => {
      if (data.ok) {
        toast.add({
          color: 'success',
          title: 'Test email sent',
          description: `Sent to ${testEmailTo.value}`,
          duration: 4000,
        })
      } else {
        toast.add({
          color: 'error',
          title: 'Failed to send',
          description: data.error,
          duration: 6000,
        })
      }
    })
    .catch((err) => {
      toast.add({
        color: 'error',
        title: 'Error',
        description: err.response?.data?.error ?? 'Failed to send test email.',
        duration: 4000,
      })
    })
    .finally(() => {
      testingEmail.value = false
    })
}

const snmpVersionOptions = [
  { label: 'v1', value: 'v1' },
  { label: 'v2c', value: 'v2c' },
  { label: 'v3', value: 'v3' },
]

const smtpTlsModeOptions = [
  { label: 'None', value: 'none' },
  { label: 'StartTLS', value: 'starttls' },
  { label: 'TLS', value: 'tls' },
]

const destinationTabItems = [
  { label: 'DNS', value: 'dns', slot: 'dns' },
  { label: 'Icinga', value: 'icinga', slot: 'icinga' },
  { label: 'LibreNMS', value: 'librenms', slot: 'librenms' },
  { label: 'Oxidized', value: 'oxidized', slot: 'oxidized' },
  { label: 'Email', value: 'email', slot: 'email' },
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
              <label for="dns_ignore_platforms" class="block font-bold mb-3">Ignore platforms</label>
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
                placeholder="One device name per line (not yet enforced by sync)"
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
              <UInput id="oxidized_dest_file" v-model="settings.oxidized_dest_file" class="w-full" />
            </div>
            <div>
              <label for="oxidized_ignore_devices" class="block font-bold mb-3">Ignore devices</label>
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
              <label for="oxidized_ignore_platforms" class="block font-bold mb-3">Ignore platforms</label>
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

        <template #email>
          <div class="flex flex-col gap-6 py-4">
            <div>
              <label for="smtp_host" class="block font-bold mb-3">SMTP host</label>
              <UInput id="smtp_host" v-model="settings.smtp_host" class="w-full" />
            </div>
            <div>
              <label for="smtp_port" class="block font-bold mb-3">SMTP port</label>
              <UInputNumber
                id="smtp_port"
                v-model="settings.smtp_port"
                :format-options="{ useGrouping: false }"
                class="w-full"
              />
            </div>
            <div>
              <label for="smtp_user" class="block font-bold mb-3">SMTP user</label>
              <UInput id="smtp_user" v-model="settings.smtp_user" class="w-full" />
            </div>
            <div>
              <label for="smtp_pass" class="block font-bold mb-3">SMTP password</label>
              <PasswordInput id="smtp_pass" v-model="settings.smtp_pass" />
            </div>
            <div>
              <label for="smtp_tls_mode" class="block font-bold mb-3">TLS mode</label>
              <USelect
                id="smtp_tls_mode"
                v-model="settings.smtp_tls_mode"
                :items="smtpTlsModeOptions"
                class="w-full"
              />
            </div>
            <div>
              <label for="email_sender" class="block font-bold mb-3">Sender (From) address</label>
              <UInput id="email_sender" v-model="settings.email_sender" class="w-full" />
            </div>
            <div>
              <label for="test_email_to" class="block font-bold mb-3">To:</label>
              <UTextarea
                id="test_email_to"
                v-model="testEmailTo"
                :rows="2"
                placeholder="One address per line"
                class="w-full"
              />
            </div>
            <div>
              <UButton
                label="Send test email"
                icon="i-lucide-send"
                color="neutral"
                :loading="testingEmail"
                :disabled="loading || !testEmailTo.trim()"
                @click="testEmail"
              />
            </div>
          </div>
        </template>
      </UTabs>
    </template>
  </div>
</template>
