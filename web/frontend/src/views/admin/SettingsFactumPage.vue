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

const smtpTlsModeOptions = [
  { label: 'None', value: 'none' },
  { label: 'StartTLS', value: 'starttls' },
  { label: 'TLS', value: 'tls' },
]

const factumTabItems = [
  { label: 'General', value: 'general', slot: 'general' },
  { label: 'Software', value: 'software', slot: 'software' },
  { label: 'Email', value: 'email', slot: 'email' },
]

function fillStorageDefaults() {
  if (!settings.storage_root) settings.storage_root = '/var/lib/factum2/storage'
  if (!settings.storage_http_listen) settings.storage_http_listen = ':8088'
  if (!settings.storage_tftp_listen) settings.storage_tftp_listen = ':69'
  if (!settings.storage_sftp_listen) settings.storage_sftp_listen = ':2222'
  if (!settings.storage_sftp_user) settings.storage_sftp_user = 'factum'
}

function onStorageToggle(val) {
  settings.storage_enabled = val
  if (val) fillStorageDefaults()
}

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
      <div class="font-semibold text-lg mb-3">Factum</div>
      <UTabs :items="factumTabItems" default-value="general">
        <template #general>
          <div class="flex flex-col gap-6 py-4">
            <div class="flex items-center gap-2">
              <USwitch
                :model-value="!!settings.organization_enabled"
                id="organization_enabled"
                @update:model-value="settings.organization_enabled = $event"
              />
              <label for="organization_enabled" class="font-bold">Organization</label>
            </div>
            <small class="text-muted-color -mt-4"
              >Customers, contacts and sites. Off by default. Turning this off hides those menu
              entries; it does not delete any data.</small
            >
            <div class="flex items-center gap-2">
              <USwitch
                :model-value="!!settings.optical_enabled"
                id="optical_enabled"
                @update:model-value="settings.optical_enabled = $event"
              />
              <label for="optical_enabled" class="font-bold">Optical / WDM modeling</label>
            </div>
            <small class="text-muted-color -mt-4"
              >ROADM, transponder/muxponder, wavelength and dark-fiber paths, maintenance impact.
              Off by default — packet-only deployments never see those screens.</small
            >
            <div class="flex items-center gap-2">
              <USwitch
                :model-value="!!settings.ipam_enabled"
                id="ipam_enabled"
                @update:model-value="settings.ipam_enabled = $event"
              />
              <label for="ipam_enabled" class="font-bold">IP address management</label>
            </div>
            <small class="text-muted-color -mt-4"
              >Namespaces, VRFs and prefix allocation. Off by default. Turning this off hides the
              UI; it does not delete any IPAM data.</small
            >
            <div class="flex items-center gap-2">
              <USwitch
                :model-value="!!settings.dns_zones_enabled"
                id="dns_zones_enabled"
                @update:model-value="settings.dns_zones_enabled = $event"
              />
              <label for="dns_zones_enabled" class="font-bold">DNS zone editor</label>
            </div>
            <small class="text-muted-color -mt-4"
              >Zones, DNS templates, SOA templates and DNSSEC policies. Off by default. Turning this
              off hides the UI; it does not delete any DNS data. Device-record sync stays on
              Destinations → DNS.</small
            >
            <div>
              <label for="factum_api_token" class="block font-bold mb-3">API token</label>
              <PasswordInput id="factum_api_token" v-model="settings.factum_api_token" />
            </div>
            <div>
              <label for="default_domain" class="block font-bold mb-3">Default domain</label>
              <UInput id="default_domain" v-model="settings.default_domain" class="w-full" />
            </div>
            <div>
              <label for="public_base_url" class="block font-bold mb-3">Public URL</label>
              <UInput
                id="public_base_url"
                v-model="settings.public_base_url"
                placeholder="https://factum.example.com"
                class="w-full"
              />
              <small class="text-muted-color"
                >Used to build absolute links in outgoing email, e.g. the password-reset link. If
                left blank, the incoming request's host is used instead.</small
              >
            </div>
            <div>
              <label for="job_history_keep" class="block font-bold mb-3">Job history to keep</label>
              <UInputNumber
                id="job_history_keep"
                v-model="settings.job_history_keep"
                :min="0"
                :format-options="{ useGrouping: false }"
                class="w-full"
              />
              <small class="text-muted-color"
                >Newest finished jobs retained when the housekeeping task runs (and their per-line
                events). 0 means 50, matching the job history list. Unfinished jobs are never
                deleted. Housekeeping does not run on its own — schedule it on the Scheduler
                page.</small
              >
            </div>
          </div>
        </template>

        <template #software>
          <div class="flex flex-col gap-6 py-4">
            <div class="flex items-center gap-2">
              <USwitch
                :model-value="!!settings.storage_enabled"
                id="storage_enabled"
                @update:model-value="onStorageToggle"
              />
              <label for="storage_enabled" class="font-bold">Software repository</label>
            </div>
            <small class="text-muted-color -mt-4"
              >Images for routers and switches. Off by default. Files live on the
              <code>factum2-storage</code> host (this primary, or a worker). Turning this off hides
              the UI; it does not delete files.</small
            >
            <div>
              <label for="storage_root" class="block font-bold mb-3">Repository directory</label>
              <UInput
                id="storage_root"
                v-model="settings.storage_root"
                placeholder="/var/lib/factum2/storage"
                class="w-full"
              />
              <small class="text-muted-color"
                >Path on the storage host, not this GUI process.</small
              >
            </div>
            <div>
              <label for="storage_http_listen" class="block font-bold mb-3">HTTP listen</label>
              <UInput
                id="storage_http_listen"
                v-model="settings.storage_http_listen"
                placeholder=":8088"
                class="w-full"
              />
              <small class="text-muted-color"
                >Device-facing HTTP. Empty disables it. Devices must be able to reach this
                address.</small
              >
            </div>
            <div>
              <label for="storage_http_url" class="block font-bold mb-3">HTTP URL (devices)</label>
              <UInput
                id="storage_http_url"
                v-model="settings.storage_http_url"
                placeholder="http://10.0.0.5:8088"
                class="w-full"
              />
            </div>
            <div>
              <label for="storage_tftp_listen" class="block font-bold mb-3">TFTP listen</label>
              <UInput
                id="storage_tftp_listen"
                v-model="settings.storage_tftp_listen"
                placeholder=":69"
                class="w-full"
              />
            </div>
            <div>
              <label for="storage_tftp_host" class="block font-bold mb-3"
                >TFTP host (devices)</label
              >
              <UInput
                id="storage_tftp_host"
                v-model="settings.storage_tftp_host"
                placeholder="10.0.0.5"
                class="w-full"
              />
            </div>
            <div>
              <label for="storage_sftp_listen" class="block font-bold mb-3">SFTP/SCP listen</label>
              <UInput
                id="storage_sftp_listen"
                v-model="settings.storage_sftp_listen"
                placeholder=":2222"
                class="w-full"
              />
            </div>
            <div>
              <label for="storage_sftp_host" class="block font-bold mb-3"
                >SFTP host (devices)</label
              >
              <UInput
                id="storage_sftp_host"
                v-model="settings.storage_sftp_host"
                placeholder="10.0.0.5:2222"
                class="w-full"
              />
            </div>
            <div>
              <label for="storage_sftp_user" class="block font-bold mb-3">SFTP user</label>
              <UInput id="storage_sftp_user" v-model="settings.storage_sftp_user" class="w-full" />
            </div>
            <div>
              <label for="storage_sftp_password" class="block font-bold mb-3">SFTP password</label>
              <PasswordInput id="storage_sftp_password" v-model="settings.storage_sftp_password" />
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
