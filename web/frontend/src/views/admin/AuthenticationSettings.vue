<script setup>
import { useToast } from '@nuxt/ui/composables'
import { onMounted, reactive, ref } from 'vue'
import { getSettings, updateSettings } from '@/api/settings'
import { testLdapConnection } from '@/api/ldap'

const toast = useToast()

const settings = reactive({})
const loading = ref(true)
const saving = ref(false)
const testing = ref(false)
const forbidden = ref(false)
const loadError = ref(false)
const showBindPassword = ref(false)

const tlsModeOptions = [
  { label: 'None', value: 'none' },
  { label: 'StartTLS', value: 'starttls' },
  { label: 'LDAPS', value: 'ldaps' },
]

const ldapServerTypeOptions = [
  { label: 'Active Directory', value: 'ad' },
  { label: 'Generic (OpenLDAP-compatible)', value: 'generic' },
]

function loadSettings() {
  loading.value = true
  forbidden.value = false
  loadError.value = false
  getSettings()
    .then((data) => {
      Object.assign(settings, data)
    })
    .catch((err) => {
      if (err.response?.status === 403 || err.response?.status === 401) {
        forbidden.value = true
      } else {
        loadError.value = true
      }
    })
    .finally(() => {
      loading.value = false
    })
}

function save() {
  saving.value = true
  updateSettings(settings)
    .then((data) => {
      Object.assign(settings, data)
      toast.add({
        color: 'success',
        title: 'Successful',
        description: 'Authentication settings saved',
        duration: 3000,
      })
    })
    .catch((err) => {
      toast.add({
        color: 'error',
        title: 'Error',
        description: err.response?.data?.error ?? 'Failed to save settings.',
        duration: 3000,
      })
    })
    .finally(() => {
      saving.value = false
    })
}

function serverAddr(server) {
  return server.port ? `${server.host}:${server.port}` : server.host
}

function testDescription(data) {
  const servers = data.servers ?? []
  if (servers.length > 1) {
    return servers
      .map((s) => (s.ok ? `${serverAddr(s)}: ok` : `${serverAddr(s)}: ${s.error}`))
      .join('; ')
  }
  if (data.ok) {
    return 'Bound to the LDAP/AD server successfully.'
  }
  return data.error
}

function testConnection() {
  testing.value = true
  testLdapConnection({
    ldap_host: settings.ldap_host,
    ldap_port: settings.ldap_port,
    ldap_host2: settings.ldap_host2,
    ldap_port2: settings.ldap_port2,
    ldap_tls_mode: settings.ldap_tls_mode,
    ldap_skip_tls_verify: settings.ldap_skip_tls_verify,
    ldap_bind_dn: settings.ldap_bind_dn,
    ldap_bind_password: settings.ldap_bind_password,
    ldap_base_dn: settings.ldap_base_dn,
  })
    .then((data) => {
      if (data.ok) {
        const n = data.servers?.length ?? 1
        toast.add({
          color: 'success',
          title: 'Connection successful',
          description:
            n > 1
              ? 'Bound to both LDAP/AD servers successfully.'
              : 'Bound to the LDAP/AD server successfully.',
          duration: 4000,
        })
      } else {
        toast.add({
          color: 'error',
          title: 'Connection failed',
          description: testDescription(data),
          duration: 6000,
        })
      }
    })
    .catch((err) => {
      toast.add({
        color: 'error',
        title: 'Error',
        description: err.response?.data?.error ?? 'Failed to test connection.',
        duration: 4000,
      })
    })
    .finally(() => {
      testing.value = false
    })
}

onMounted(loadSettings)
</script>

<template>
  <div v-if="forbidden" class="card">
    <UAlert
      color="error"
      variant="subtle"
      title="You need administrator permissions to view authentication settings."
    />
  </div>
  <div v-else-if="loadError" class="card">
    <UAlert color="error" variant="subtle" title="Failed to load settings." />
  </div>
  <div v-else class="card">
    <div class="flex items-center justify-between mb-6">
      <UButton
        label="Test Connection"
        icon="i-lucide-zap"
        color="neutral"
        :loading="testing"
        :disabled="loading"
        @click="testConnection"
      />
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
      <div class="flex flex-col gap-6">
        <div class="flex items-center gap-2">
          <USwitch v-model="settings.ldap_enabled" id="ldap_enabled" />
          <label for="ldap_enabled" class="font-bold">Enabled</label>
        </div>

        <div>
          <label class="block font-bold mb-3">Server type</label>
          <URadioGroup
            v-model="settings.ldap_server_type"
            :items="ldapServerTypeOptions"
            orientation="horizontal"
          />
        </div>

        <div class="grid grid-cols-1 md:grid-cols-3 gap-6">
          <div class="md:col-span-2">
            <label for="ldap_host" class="block font-bold mb-3">Primary host</label>
            <UInput
              id="ldap_host"
              v-model="settings.ldap_host"
              placeholder="dc1.example.com"
              class="w-full"
            />
          </div>
          <div>
            <label for="ldap_port" class="block font-bold mb-3">Port</label>
            <UInputNumber
              id="ldap_port"
              v-model="settings.ldap_port"
              :format-options="{ useGrouping: false }"
              class="w-full"
            />
          </div>
        </div>

        <div class="grid grid-cols-1 md:grid-cols-3 gap-6">
          <div class="md:col-span-2">
            <label for="ldap_host2" class="block font-bold mb-3">Secondary host</label>
            <UInput
              id="ldap_host2"
              v-model="settings.ldap_host2"
              placeholder="dc2.example.com"
              class="w-full"
            />
            <small class="text-muted-color"
              >Optional second server for redundancy. Used if the primary is unreachable.</small
            >
          </div>
          <div>
            <label for="ldap_port2" class="block font-bold mb-3">Secondary port</label>
            <UInputNumber
              id="ldap_port2"
              v-model="settings.ldap_port2"
              :format-options="{ useGrouping: false }"
              class="w-full"
            />
            <small class="text-muted-color">Leave 0 to use the primary port.</small>
          </div>
        </div>

        <div class="grid grid-cols-1 md:grid-cols-2 gap-6">
          <div>
            <label for="ldap_tls_mode" class="block font-bold mb-3">TLS mode</label>
            <USelect
              id="ldap_tls_mode"
              v-model="settings.ldap_tls_mode"
              :items="tlsModeOptions"
              class="w-full"
            />
          </div>
          <div class="flex items-center gap-2 mt-8">
            <USwitch v-model="settings.ldap_skip_tls_verify" id="ldap_skip_tls_verify" />
            <label for="ldap_skip_tls_verify" class="font-bold"
              >Skip TLS certificate verification</label
            >
          </div>
        </div>

        <div>
          <label for="ldap_bind_dn" class="block font-bold mb-3">Service account bind DN</label>
          <UInput
            id="ldap_bind_dn"
            v-model="settings.ldap_bind_dn"
            placeholder="CN=svc-factum,OU=service accounts,DC=example,DC=com"
            class="w-full"
          />
        </div>
        <div>
          <label for="ldap_bind_password" class="block font-bold mb-3"
            >Service account password</label
          >
          <UInput
            id="ldap_bind_password"
            v-model="settings.ldap_bind_password"
            :type="showBindPassword ? 'text' : 'password'"
            class="w-full"
            :ui="{ trailing: 'pe-1' }"
          >
            <template #trailing>
              <UButton
                color="neutral"
                variant="link"
                size="sm"
                :icon="showBindPassword ? 'i-lucide-eye-off' : 'i-lucide-eye'"
                @click="showBindPassword = !showBindPassword"
              />
            </template>
          </UInput>
        </div>
        <div>
          <label for="ldap_base_dn" class="block font-bold mb-3">Base DN</label>
          <UInput
            id="ldap_base_dn"
            v-model="settings.ldap_base_dn"
            placeholder="DC=example,DC=com"
            class="w-full"
          />
        </div>
        <div>
          <label for="ldap_user_filter" class="block font-bold mb-3">User search filter</label>
          <UInput
            id="ldap_user_filter"
            v-model="settings.ldap_user_filter"
            placeholder="(sAMAccountName=%s)"
            class="w-full"
          />
          <small class="text-muted-color"
            >One %s placeholder for the (escaped) username. Use "(sAMAccountName=%s)" for Active
            Directory or "(uid=%s)" for OpenLDAP.</small
          >
        </div>

        <div>
          <small class="text-muted-color"
            >Attribute names below default to the value shown as a placeholder when left
            blank.</small
          >
        </div>
        <div class="grid grid-cols-1 md:grid-cols-4 gap-6">
          <div>
            <label for="ldap_attr_username" class="block font-bold mb-3">Username attribute</label>
            <UInput
              id="ldap_attr_username"
              v-model="settings.ldap_attr_username"
              :placeholder="settings.ldap_server_type === 'generic' ? 'uid' : 'sAMAccountName'"
              class="w-full"
            />
            <small class="text-muted-color"
              >Only used to look up an LDAP user by email for the forgot-password flow, before
              they've ever logged in.</small
            >
          </div>
          <div>
            <label for="ldap_attr_email" class="block font-bold mb-3">Email attribute</label>
            <UInput
              id="ldap_attr_email"
              v-model="settings.ldap_attr_email"
              placeholder="mail"
              class="w-full"
            />
          </div>
          <div>
            <label for="ldap_attr_display_name" class="block font-bold mb-3"
              >Display name attribute</label
            >
            <UInput
              id="ldap_attr_display_name"
              v-model="settings.ldap_attr_display_name"
              placeholder="displayName"
              class="w-full"
            />
          </div>
          <div>
            <label for="ldap_attr_mobile" class="block font-bold mb-3">Mobile attribute</label>
            <UInput
              id="ldap_attr_mobile"
              v-model="settings.ldap_attr_mobile"
              placeholder="mobile"
              class="w-full"
            />
          </div>
          <div>
            <label for="ldap_attr_groups" class="block font-bold mb-3">Groups attribute</label>
            <UInput
              id="ldap_attr_groups"
              v-model="settings.ldap_attr_groups"
              placeholder="memberOf"
              class="w-full"
            />
          </div>
        </div>

        <div class="border-t border-default pt-6">
          <div class="flex items-center gap-2">
            <USwitch
              v-model="settings.ldap_allow_password_change"
              id="ldap_allow_password_change"
            />
            <label for="ldap_allow_password_change" class="font-bold"
              >Allow password changes to be written back to LDAP/AD</label
            >
          </div>
          <small class="text-muted-color">
            Lets LDAP/AD-backed users change their password from Factum (self-service "Change
            password" on the User Settings page, and the forgot-password flow) instead of being
            refused. The elevated directory account that actually performs the write is
            <strong>not</strong> configured here - it must be set as
            <code>ldap_writeback.bind_dn</code> / <code>ldap_writeback.bind_password</code> in the
            server's own config file by someone with filesystem access, since it needs far more
            privilege than the read-only service account above. Enabling this switch without also
            setting those has no effect.
          </small>
        </div>
      </div>
    </template>
  </div>
</template>
