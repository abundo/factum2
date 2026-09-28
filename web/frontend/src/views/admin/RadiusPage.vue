<script setup>
import { useToast } from '@nuxt/ui/composables'
import { onMounted, ref } from 'vue'
import {
  createRadiusClient,
  createRadiusPolicy,
  deleteRadiusClient,
  deleteRadiusPolicy,
  getRadiusClientSecret,
  getRadiusClients,
  getRadiusDeviceRoles,
  getRadiusEvents,
  getRadiusPolicies,
  updateRadiusClient,
  updateRadiusPolicy,
} from '@/api/radius'
import FormModal from '@/components/FormModal.vue'
import PasswordInput from '@/components/PasswordInput.vue'
import { useSettings } from '@/composables/useSettings'

const toast = useToast()
const { settings, loading, saving, forbidden, loadError, save } = useSettings()

const tab = ref('service')
const tabs = [
  { label: 'Service', value: 'service', slot: 'service' },
  { label: 'Clients', value: 'clients', slot: 'clients' },
  { label: 'Groups', value: 'groups', slot: 'groups' },
  { label: 'Log', value: 'log', slot: 'log' },
]

const clients = ref([])
const policies = ref([])
const events = ref([])
const deviceRoles = ref([])
const clientsError = ref(null)
const policiesError = ref(null)
const eventsError = ref(null)

const clientDialog = ref(false)
const client = ref({})
const clientSubmitted = ref(false)
const clientSaving = ref(false)

const policyDialog = ref(false)
const policy = ref({})
const policySubmitted = ref(false)
const policySaving = ref(false)

const clientColumns = [
  { id: 'actions', header: '' },
  { accessorKey: 'name', header: 'Name' },
  { accessorKey: 'address', header: 'Address' },
  { id: 'enabled', header: 'Enabled' },
]

const policyColumns = [
  { id: 'actions', header: '' },
  { accessorKey: 'group_dn', header: 'LDAP group' },
  { id: 'access', header: 'Devices' },
]

const eventColumns = [
  { id: 'when', header: 'Time' },
  { accessorKey: 'username', header: 'User' },
  { accessorKey: 'device_name', header: 'Device' },
  { accessorKey: 'device_role', header: 'Role' },
  { accessorKey: 'nas_ip', header: 'NAS' },
  { id: 'result', header: 'Result' },
  { accessorKey: 'reason', header: 'Reason' },
  { accessorKey: 'worker', header: 'Worker' },
]

function onRadiusToggle(val) {
  settings.radius_enabled = val
  if (val && !settings.radius_listen) settings.radius_listen = ':1812'
}

function loadClients() {
  clientsError.value = null
  getRadiusClients()
    .then((data) => {
      clients.value = data ?? []
    })
    .catch(() => {
      clientsError.value = 'Failed to load RADIUS clients.'
    })
}

function loadPolicies() {
  policiesError.value = null
  getRadiusPolicies()
    .then((data) => {
      policies.value = data ?? []
    })
    .catch(() => {
      policiesError.value = 'Failed to load group rules.'
    })
}

function loadEvents() {
  eventsError.value = null
  getRadiusEvents()
    .then((data) => {
      events.value = data ?? []
    })
    .catch(() => {
      eventsError.value = 'Failed to load the login log.'
    })
}

function loadRoles() {
  getRadiusDeviceRoles()
    .then((data) => {
      deviceRoles.value = data ?? []
    })
    .catch(() => {
      deviceRoles.value = []
    })
}

function onTab(value) {
  if (value === 'log') loadEvents()
  if (value === 'clients') loadClients()
  if (value === 'groups') loadPolicies()
}

function openClient() {
  client.value = { enabled: true, name: '', address: '', secret: '' }
  clientSubmitted.value = false
  clientDialog.value = true
}

function editClient(row) {
  client.value = { ...row, secret: '' }
  clientSubmitted.value = false
  clientDialog.value = true
  getRadiusClientSecret(row.id)
    .then((data) => {
      client.value.secret = data.secret
    })
    .catch(() => {})
}

function saveClient() {
  clientSubmitted.value = true
  if (!client.value.name?.trim() || !client.value.address?.trim()) return
  if (!client.value.id && !client.value.secret) return
  clientSaving.value = true
  const payload = { ...client.value }
  if (!payload.secret) delete payload.secret
  const request = payload.id ? updateRadiusClient(payload.id, payload) : createRadiusClient(payload)
  request
    .then(() => {
      toast.add({
        color: 'success',
        title: 'Saved',
        description: 'RADIUS client saved',
        duration: 3000,
      })
      clientDialog.value = false
      loadClients()
    })
    .catch((err) => {
      toast.add({
        color: 'error',
        title: 'Error',
        description: err.response?.data?.error ?? 'Failed to save client.',
        duration: 4000,
      })
    })
    .finally(() => {
      clientSaving.value = false
    })
}

function removeClient(row) {
  if (!window.confirm(`Remove RADIUS client ${row.name}?`)) return
  deleteRadiusClient(row.id)
    .then(() => loadClients())
    .catch((err) => {
      toast.add({
        color: 'error',
        title: 'Error',
        description: err.response?.data?.error ?? 'Failed to remove client.',
        duration: 4000,
      })
    })
}

function openPolicy() {
  policy.value = { group_dn: '', all_devices: false, rolesText: '' }
  policySubmitted.value = false
  policyDialog.value = true
}

function editPolicy(row) {
  policy.value = {
    ...row,
    rolesText: (row.roles ?? []).join('\n'),
  }
  policySubmitted.value = false
  policyDialog.value = true
}

function savePolicy() {
  policySubmitted.value = true
  const roles = (policy.value.rolesText ?? '')
    .split(/[\n,]/)
    .map((r) => r.trim())
    .filter(Boolean)
  if (!policy.value.group_dn?.trim()) return
  if (!policy.value.all_devices && roles.length === 0) return
  policySaving.value = true
  const payload = {
    group_dn: policy.value.group_dn,
    all_devices: !!policy.value.all_devices,
    roles,
  }
  const request = policy.value.id
    ? updateRadiusPolicy(policy.value.id, payload)
    : createRadiusPolicy(payload)
  request
    .then(() => {
      toast.add({
        color: 'success',
        title: 'Saved',
        description: 'Group rule saved',
        duration: 3000,
      })
      policyDialog.value = false
      loadPolicies()
    })
    .catch((err) => {
      toast.add({
        color: 'error',
        title: 'Error',
        description: err.response?.data?.error ?? 'Failed to save group rule.',
        duration: 4000,
      })
    })
    .finally(() => {
      policySaving.value = false
    })
}

function removePolicy(row) {
  if (!window.confirm(`Remove ${row.group_dn}?`)) return
  deleteRadiusPolicy(row.id)
    .then(() => loadPolicies())
    .catch((err) => {
      toast.add({
        color: 'error',
        title: 'Error',
        description: err.response?.data?.error ?? 'Failed to remove group rule.',
        duration: 4000,
      })
    })
}

function accessLabel(row) {
  if (row.all_devices) return 'All devices'
  return (row.roles ?? []).join(', ')
}

function when(value) {
  if (!value) return ''
  const d = new Date(value)
  if (Number.isNaN(d.getTime())) return value
  return d.toLocaleString()
}

onMounted(() => {
  loadClients()
  loadPolicies()
  loadEvents()
  loadRoles()
})
</script>

<template>
  <div v-if="forbidden" class="card">
    <UAlert color="error" variant="subtle" title="You need administrator permissions." />
  </div>
  <div v-else-if="loadError" class="card">
    <UAlert color="error" variant="subtle" title="Failed to load settings." />
  </div>
  <div v-else class="card">
    <h4 class="m-0 mb-4">RADIUS</h4>
    <UTabs v-model="tab" :items="tabs" @update:model-value="onTab">
      <template #service>
        <div class="flex flex-col gap-6 py-4 max-w-3xl">
          <div class="flex items-center gap-2">
            <USwitch
              :model-value="!!settings.radius_enabled"
              id="radius_enabled"
              @update:model-value="onRadiusToggle"
            />
            <label for="radius_enabled" class="font-bold">RADIUS login</label>
          </div>
          <small class="text-muted-color -mt-4">
            Workers with a <code>radius</code> command listen for switch and router logins and bind
            to the directory configured under Authentication. factum2 is not on that path: each
            worker keeps the last policy on disk and keeps answering while this server is down.
          </small>
          <div>
            <label for="radius_listen" class="block font-bold mb-3">Listen address</label>
            <UInput
              id="radius_listen"
              v-model="settings.radius_listen"
              placeholder=":1812"
              class="w-full"
              :disabled="loading"
            />
            <small class="text-muted-color"
              >UDP address on every RADIUS worker. Default is :1812.</small
            >
          </div>
          <div>
            <label for="radius_machine_account" class="block font-bold mb-3"
              >AD computer account</label
            >
            <UInput
              id="radius_machine_account"
              v-model="settings.radius_machine_account"
              placeholder="FACTUM-RADIUS$"
              class="w-full"
              :disabled="loading"
            />
            <small class="text-muted-color">
              Used only for MikroTik MS-CHAPv2. Create a computer account in Active Directory and
              put its name here, with the trailing $. Leave empty to refuse MS-CHAPv2. PAP logins do
              not use it. OpenLDAP cannot check MS-CHAPv2.
            </small>
          </div>
          <div>
            <label for="radius_machine_password" class="block font-bold mb-3"
              >Computer account password</label
            >
            <PasswordInput
              id="radius_machine_password"
              v-model="settings.radius_machine_password"
              :disabled="loading"
            />
          </div>
          <div>
            <label for="radius_machine_domain" class="block font-bold mb-3">AD domain</label>
            <UInput
              id="radius_machine_domain"
              v-model="settings.radius_machine_domain"
              placeholder="CORP"
              class="w-full"
              :disabled="loading"
            />
            <small class="text-muted-color">
              NetBIOS domain of the computer account. Empty uses the DNS domain from the LDAP base
              DN.
            </small>
          </div>
          <div>
            <label for="radius_reply" class="block font-bold mb-3">Accept attributes</label>
            <UTextarea
              id="radius_reply"
              v-model="settings.radius_reply"
              :rows="16"
              class="w-full font-mono text-sm"
              :disabled="loading"
            />
            <small class="text-muted-color">
              Added to every Access-Accept. A switch uses the attributes it knows and ignores the
              rest. One attribute per line. <code>#</code> comments a line out. Repeat a name to
              send it twice. <code>26.&lt;vendor&gt;.&lt;type&gt; = "text"</code> sends an attribute
              that is not in the built-in list. Clear the box and save to send none.
            </small>
          </div>
          <UButton label="Save" icon="i-lucide-check" :loading="saving" @click="save" />
          <div class="text-sm text-muted-color flex flex-col gap-2">
            <p>
              Switches must use PAP and send Message-Authenticator. A user is accepted only when the
              password binds and one of their LDAP groups is allowed for that device's NetBox role.
              An unknown address, a disabled device, or a role with no matching group is rejected.
            </p>
            <p>
              On the worker, add a <code>radius</code> entry under <code>worker.commands</code>. The
              command itself is not run; it marks the host as a RADIUS listener. Point each switch
              at that host, port 1812.
            </p>
          </div>
        </div>
      </template>

      <template #clients>
        <div class="py-4">
          <div class="flex items-center gap-2 mb-4">
            <UButton
              label="New"
              icon="i-lucide-plus"
              color="neutral"
              size="sm"
              @click="openClient"
            />
            <small class="text-muted-color">
              The address is the IP the switch sends from. It must also be an address on that device
              in NetBox.
            </small>
          </div>
          <UAlert
            v-if="clientsError"
            color="error"
            variant="subtle"
            :title="clientsError"
            class="mb-4"
          />
          <UTable :data="clients" :columns="clientColumns" :empty="'No RADIUS clients yet.'">
            <template #actions-cell="{ row }">
              <div class="flex gap-1">
                <UButton
                  icon="i-lucide-pencil"
                  variant="outline"
                  color="neutral"
                  size="sm"
                  @click="editClient(row.original)"
                />
                <UButton
                  icon="i-lucide-trash"
                  variant="outline"
                  color="error"
                  size="sm"
                  @click="removeClient(row.original)"
                />
              </div>
            </template>
            <template #enabled-cell="{ row }">
              <UBadge
                :label="row.original.enabled ? 'Yes' : 'No'"
                :color="row.original.enabled ? 'success' : 'neutral'"
                variant="subtle"
              />
            </template>
          </UTable>
        </div>
      </template>

      <template #groups>
        <div class="py-4">
          <div class="flex flex-wrap items-center gap-2 mb-4">
            <UButton
              label="New"
              icon="i-lucide-plus"
              color="neutral"
              size="sm"
              @click="openPolicy"
            />
            <small v-if="deviceRoles.length" class="text-muted-color">
              Roles in inventory: {{ deviceRoles.join(', ') }}
            </small>
          </div>
          <UAlert
            v-if="policiesError"
            color="error"
            variant="subtle"
            :title="policiesError"
            class="mb-4"
          />
          <UTable :data="policies" :columns="policyColumns" :empty="'No group rules yet.'">
            <template #actions-cell="{ row }">
              <div class="flex gap-1">
                <UButton
                  icon="i-lucide-pencil"
                  variant="outline"
                  color="neutral"
                  size="sm"
                  @click="editPolicy(row.original)"
                />
                <UButton
                  icon="i-lucide-trash"
                  variant="outline"
                  color="error"
                  size="sm"
                  @click="removePolicy(row.original)"
                />
              </div>
            </template>
            <template #access-cell="{ row }">
              {{ accessLabel(row.original) }}
            </template>
          </UTable>
        </div>
      </template>

      <template #log>
        <div class="py-4">
          <div class="flex items-center gap-2 mb-4">
            <UButton
              label="Refresh"
              icon="i-lucide-refresh-cw"
              color="neutral"
              size="sm"
              variant="outline"
              @click="loadEvents"
            />
          </div>
          <UAlert
            v-if="eventsError"
            color="error"
            variant="subtle"
            :title="eventsError"
            class="mb-4"
          />
          <UTable :data="events" :columns="eventColumns" :empty="'No login attempts yet.'">
            <template #when-cell="{ row }">
              {{ when(row.original.reported_at) }}
            </template>
            <template #result-cell="{ row }">
              <UBadge
                :label="row.original.result"
                :color="row.original.result === 'accept' ? 'success' : 'error'"
                variant="subtle"
              />
            </template>
          </UTable>
        </div>
      </template>
    </UTabs>
  </div>

  <FormModal v-model:open="clientDialog" :source="client" title="RADIUS client">
    <template #body>
      <div class="flex flex-col gap-6">
        <div>
          <label for="client-name" class="block font-bold mb-3">Name</label>
          <UInput id="client-name" v-model.trim="client.name" class="w-full" autofocus />
          <small v-if="clientSubmitted && !client.name?.trim()" class="text-red-500"
            >Name is required.</small
          >
        </div>
        <div>
          <label for="client-address" class="block font-bold mb-3">Address</label>
          <UInput
            id="client-address"
            v-model.trim="client.address"
            placeholder="10.0.0.5"
            class="w-full"
          />
          <small v-if="clientSubmitted && !client.address?.trim()" class="text-red-500"
            >Address is required.</small
          >
        </div>
        <div>
          <label for="client-secret" class="block font-bold mb-3">Shared secret</label>
          <PasswordInput id="client-secret" v-model="client.secret" />
          <small v-if="clientSubmitted && !client.id && !client.secret" class="text-red-500"
            >Secret is required.</small
          >
          <small v-else-if="client.id" class="text-muted-color"
            >Leave blank to keep the current secret.</small
          >
        </div>
        <div class="flex items-center gap-3">
          <USwitch v-model="client.enabled" id="client-enabled" />
          <label for="client-enabled" class="font-bold">Enabled</label>
        </div>
      </div>
    </template>
    <template #footer>
      <UButton label="Cancel" icon="i-lucide-x" variant="ghost" @click="clientDialog = false" />
      <UButton label="Save" icon="i-lucide-check" :loading="clientSaving" @click="saveClient" />
    </template>
  </FormModal>

  <FormModal v-model:open="policyDialog" :source="policy" title="LDAP group">
    <template #body>
      <div class="flex flex-col gap-6">
        <div>
          <label for="group-dn" class="block font-bold mb-3">Group DN</label>
          <UInput
            id="group-dn"
            v-model.trim="policy.group_dn"
            placeholder="CN=wdm-ops,OU=Groups,DC=example,DC=com"
            class="w-full"
            autofocus
          />
          <small v-if="policySubmitted && !policy.group_dn?.trim()" class="text-red-500"
            >Group DN is required.</small
          >
        </div>
        <div class="flex items-center gap-3">
          <USwitch v-model="policy.all_devices" id="all-devices" />
          <label for="all-devices" class="font-bold">All devices</label>
        </div>
        <small class="text-muted-color -mt-4">Administrators who may log in to every role.</small>
        <div v-if="!policy.all_devices">
          <label for="roles" class="block font-bold mb-3">Device roles</label>
          <UTextarea
            id="roles"
            v-model="policy.rolesText"
            :rows="4"
            placeholder="WDM"
            class="w-full"
          />
          <small class="text-muted-color"
            >One NetBox device role per line. Matching is case-insensitive.</small
          >
          <small
            v-if="policySubmitted && !policy.all_devices && !policy.rolesText?.trim()"
            class="text-red-500 block"
            >Enter a role, or allow all devices.</small
          >
        </div>
      </div>
    </template>
    <template #footer>
      <UButton label="Cancel" icon="i-lucide-x" variant="ghost" @click="policyDialog = false" />
      <UButton label="Save" icon="i-lucide-check" :loading="policySaving" @click="savePolicy" />
    </template>
  </FormModal>
</template>
