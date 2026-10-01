<script setup>
import { useToast } from '@nuxt/ui/composables'
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
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
import LdapTreeBrowser from '@/components/LdapTreeBrowser.vue'
import PasswordInput from '@/components/PasswordInput.vue'
import SearchInput from '@/components/SearchInput.vue'
import { useConfirm } from '@/composables/useConfirm'
import { useSettings } from '@/composables/useSettings'
import { parseReply, serializeReply } from '@/utils/radiusReply'
import { useSearch, valuesText } from '@/utils/search'

const toast = useToast()
const { confirmDelete } = useConfirm()
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
const clientDeleting = ref(false)

const policyDialog = ref(false)
const policy = ref({})
const policySubmitted = ref(false)
const policySaving = ref(false)
const policyDeleting = ref(false)
const browserVisible = ref(false)

const replyRows = ref([])
let replySeq = 1
let hydratingReply = false

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

function stampReply(rows) {
  return rows.map((row) => ({ ...row, id: replySeq++ }))
}

function blankReply(commentOnly) {
  return {
    id: replySeq++,
    enabled: !commentOnly,
    name: '',
    op: '=',
    value: '',
    quoted: false,
    comment: '',
  }
}

function addReply() {
  replyRows.value.push(blankReply(false))
}

function addReplyComment() {
  const row = blankReply(true)
  row.enabled = false
  replyRows.value.push(row)
}

function removeReply(index) {
  replyRows.value.splice(index, 1)
}

const replyColumns = [
  {
    id: 'drag',
    header: '',
    meta: { class: { th: 'w-7', td: 'w-7 text-center' } },
  },
  {
    id: 'enabled',
    header: 'Send',
    meta: { class: { th: 'w-14 text-center', td: 'w-14 text-center' } },
  },
  { accessorKey: 'name', header: 'Attribute' },
  { accessorKey: 'value', header: 'Value' },
  { accessorKey: 'comment', header: 'Comment' },
  {
    id: 'actions',
    header: '',
    meta: { class: { th: 'w-10', td: 'w-10 text-center' } },
  },
]

const replyCellUi = {
  root: 'w-full',
  base: 'rounded-none h-8 px-2',
}

const replyTableUi = {
  root: 'w-full overflow-visible',
  base: 'table-fixed w-full min-w-full',
  thead: 'sticky top-0 z-10 bg-elevated',
  tbody: 'divide-y-0',
  separator: 'hidden',
  th: 'px-2 py-1.5 text-xs font-medium bg-elevated',
  td: 'p-0 min-w-0',
}

const replyTableMeta = {
  class: {
    tr: (row) => (row.original.name.trim() && !row.original.enabled ? 'opacity-60' : ''),
  },
}

function replyRowId(row) {
  return String(row.id)
}

const DRAG_THRESHOLD_PX = 4
let dragFrom = null
let dragMoved = false
let dragStartY = 0
let dropLine = null

function replyRowElements() {
  return [...document.querySelectorAll('.radius-reply-table [data-slot="tbody"] [data-slot="tr"]')]
}

function gapFromY(clientY) {
  const rows = replyRowElements()
  for (let i = 0; i < rows.length; i++) {
    const box = rows[i].getBoundingClientRect()
    if (clientY < box.top + box.height / 2) return i
  }
  return rows.length
}

function ensureDropLine() {
  if (dropLine) return
  dropLine = document.createElement('div')
  dropLine.className = 'radius-reply-drop-line'
  document.body.appendChild(dropLine)
}

function paintSource(index) {
  replyRowElements().forEach((el, i) => {
    el.classList.toggle('radius-reply-dragging', i === index)
  })
}

function paintDrop(clientY) {
  const rows = replyRowElements()
  const table = document.querySelector('.radius-reply-table')
  if (!rows.length || !table || !dropLine) return
  const gap = gapFromY(clientY)
  const edge =
    gap <= 0
      ? rows[0].getBoundingClientRect().top
      : rows[Math.min(gap, rows.length) - 1].getBoundingClientRect().bottom
  const box = table.getBoundingClientRect()
  dropLine.style.opacity = '1'
  dropLine.style.width = `${box.width}px`
  dropLine.style.transform = `translate(${box.left}px, ${edge - 1}px)`
}

function endReplyDrag() {
  window.removeEventListener('pointermove', onReplyDragMove, true)
  window.removeEventListener('pointerup', onReplyDragUp, true)
  window.removeEventListener('pointercancel', onReplyDragUp, true)
  document.body.style.cursor = ''
  document.body.style.userSelect = ''
  replyRowElements().forEach((el) => el.classList.remove('radius-reply-dragging'))
  if (dropLine) dropLine.style.opacity = '0'
  dragFrom = null
  dragMoved = false
}

function onReplyDragMove(event) {
  if (dragFrom == null) return
  if (!dragMoved && Math.abs(event.clientY - dragStartY) < DRAG_THRESHOLD_PX) return
  if (!dragMoved) {
    dragMoved = true
    document.body.style.cursor = 'grabbing'
    document.body.style.userSelect = 'none'
    ensureDropLine()
  }
  event.preventDefault()
  paintSource(dragFrom)
  paintDrop(event.clientY)
}

function onReplyDragUp(event) {
  const from = dragFrom
  const gap = dragMoved ? gapFromY(event.clientY) : -1
  const to = gap > from ? gap - 1 : gap
  endReplyDrag()
  if (from == null || gap < 0 || from === to) return
  const next = replyRows.value.slice()
  const [item] = next.splice(from, 1)
  next.splice(to, 0, item)
  replyRows.value = next
}

function onReplyDragDown(index, event) {
  if (loading.value || event.button !== 0) return
  event.preventDefault()
  event.currentTarget.setPointerCapture?.(event.pointerId)
  dragFrom = index
  dragMoved = false
  dragStartY = event.clientY
  window.addEventListener('pointermove', onReplyDragMove, { passive: false, capture: true })
  window.addEventListener('pointerup', onReplyDragUp, true)
  window.addEventListener('pointercancel', onReplyDragUp, true)
}

function onReplyName(row, value) {
  const wasEmpty = !row.name.trim()
  row.name = value
  if (wasEmpty && value.trim()) row.enabled = true
}

// Sync, not the default pre flush: the grid must not write back while the
// stored text is being copied into the rows.
watch(
  () => settings.radius_reply,
  (text) => {
    const stored = (text ?? '').replace(/\s+$/, '')
    if (serializeReply(replyRows.value) === stored) return
    hydratingReply = true
    replyRows.value = stampReply(parseReply(stored))
    hydratingReply = false
  },
  { flush: 'sync' },
)

watch(
  replyRows,
  () => {
    if (hydratingReply || loading.value) return
    const text = serializeReply(replyRows.value)
    if ((settings.radius_reply ?? '') !== text) settings.radius_reply = text
  },
  { deep: true, flush: 'sync' },
)

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

async function removeClient() {
  if (!client.value.id) return
  const name = client.value.name?.trim() || 'client'
  if (!(await confirmDelete(`RADIUS client ${name}`))) return
  clientDeleting.value = true
  deleteRadiusClient(client.value.id)
    .then(() => {
      clientDialog.value = false
      loadClients()
    })
    .catch((err) => {
      toast.add({
        color: 'error',
        title: 'Error',
        description: err.response?.data?.error ?? 'Failed to remove client.',
        duration: 4000,
      })
    })
    .finally(() => {
      clientDeleting.value = false
    })
}

function openPolicy() {
  policy.value = { group_dn: '', all_devices: false, rolesText: '' }
  policySubmitted.value = false
  policyDialog.value = true
}

function onGroupDnSelected(dn) {
  policy.value.group_dn = dn
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

async function removePolicy() {
  if (!policy.value.id) return
  const name = policy.value.group_dn?.trim() || 'group rule'
  if (!(await confirmDelete(`group rule ${name}`))) return
  policyDeleting.value = true
  deleteRadiusPolicy(policy.value.id)
    .then(() => {
      policyDialog.value = false
      loadPolicies()
    })
    .catch((err) => {
      toast.add({
        color: 'error',
        title: 'Error',
        description: err.response?.data?.error ?? 'Failed to remove group rule.',
        duration: 4000,
      })
    })
    .finally(() => {
      policyDeleting.value = false
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

const { search: clientSearch, filtered: filteredClients } = useSearch(clients, (row) =>
  valuesText(row.name, row.address, row.enabled ? 'Yes' : 'No'),
)
const { search: policySearch, filtered: filteredPolicies } = useSearch(policies, (row) =>
  valuesText(row.group_dn, accessLabel(row)),
)
const { search: eventSearch, filtered: filteredEvents } = useSearch(events, (row) =>
  valuesText(
    when(row.reported_at),
    row.username,
    row.device_name,
    row.device_role,
    row.nas_ip,
    row.result,
    row.reason,
    row.worker,
  ),
)

onMounted(() => {
  loadClients()
  loadPolicies()
  loadEvents()
  loadRoles()
})

onBeforeUnmount(() => {
  endReplyDrag()
  dropLine?.remove()
  dropLine = null
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
        <div class="flex flex-col gap-6 py-4">
          <div class="flex flex-col gap-6 max-w-3xl">
            <div class="flex items-center gap-2">
              <USwitch
                :model-value="!!settings.radius_enabled"
                id="radius_enabled"
                @update:model-value="onRadiusToggle"
              />
              <label for="radius_enabled" class="font-bold">RADIUS login</label>
            </div>
            <small class="text-muted-color -mt-4">
              Workers with a <code>radius</code> command listen for switch and router logins and
              bind to the directory configured under Authentication. factum2 is not on that path:
              each worker keeps the last policy on disk and keeps answering while this server is
              down.
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
                put its name here, with the trailing $. Leave empty to refuse MS-CHAPv2. PAP logins
                do not use it. OpenLDAP cannot check MS-CHAPv2.
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
          </div>
          <div>
            <div class="flex flex-wrap items-center gap-2 mb-3">
              <span class="font-bold">Accept attributes</span>
              <UButton
                label="Add attribute"
                icon="i-lucide-plus"
                color="neutral"
                size="sm"
                :disabled="loading"
                @click="addReply"
              />
              <UButton
                label="Add comment"
                icon="i-lucide-message-square-plus"
                color="neutral"
                variant="outline"
                size="sm"
                :disabled="loading"
                @click="addReplyComment"
              />
            </div>
            <div
              class="radius-reply-table w-full rounded-md ring ring-default overflow-auto max-h-[min(32rem,calc(100dvh-16rem))]"
            >
              <UTable
                :data="replyRows"
                :columns="replyColumns"
                :get-row-id="replyRowId"
                :ui="replyTableUi"
                :meta="replyTableMeta"
                :watch-options="{ deep: false }"
                empty="No attributes. An accepted login gets none until you add a row."
              >
                <template #drag-cell="{ row }">
                  <span
                    class="inline-flex items-center justify-center size-8 text-muted cursor-grab active:cursor-grabbing select-none touch-none"
                    :class="loading && 'pointer-events-none'"
                    title="Drag to reorder"
                    aria-label="Drag to reorder"
                    @pointerdown="onReplyDragDown(row.index, $event)"
                  >
                    <UIcon name="i-lucide-grip-vertical" class="size-3.5 pointer-events-none" />
                  </span>
                </template>
                <template #enabled-cell="{ row }">
                  <div class="flex items-center justify-center h-8">
                    <UCheckbox
                      v-model="row.original.enabled"
                      :disabled="loading || !row.original.name.trim()"
                      :aria-label="`Send ${row.original.name || 'row ' + (row.index + 1)}`"
                    />
                  </div>
                </template>
                <template #name-cell="{ row }">
                  <UInput
                    :model-value="row.original.name"
                    placeholder="Service-Type"
                    class="w-full font-mono"
                    variant="none"
                    color="neutral"
                    size="xs"
                    :ui="replyCellUi"
                    :disabled="loading"
                    @update:model-value="(value) => onReplyName(row.original, value)"
                  />
                </template>
                <template #value-cell="{ row }">
                  <UInput
                    v-model="row.original.value"
                    placeholder="NAS-Prompt-User"
                    class="w-full font-mono"
                    variant="none"
                    color="neutral"
                    size="xs"
                    :ui="replyCellUi"
                    :disabled="loading || !row.original.name.trim()"
                  />
                </template>
                <template #comment-cell="{ row }">
                  <UInput
                    v-model="row.original.comment"
                    placeholder="Comment"
                    class="w-full"
                    variant="none"
                    color="neutral"
                    size="xs"
                    :ui="replyCellUi"
                    :disabled="loading"
                  />
                </template>
                <template #actions-header>
                  <div class="flex items-center justify-center">
                    <UButton
                      type="button"
                      size="xs"
                      color="neutral"
                      variant="ghost"
                      icon="i-lucide-plus"
                      :disabled="loading"
                      aria-label="Add attribute"
                      title="Add attribute"
                      @click="addReply"
                    />
                  </div>
                </template>
                <template #actions-cell="{ row }">
                  <div class="flex items-center justify-center h-8">
                    <UButton
                      type="button"
                      size="xs"
                      color="error"
                      variant="ghost"
                      icon="i-lucide-trash"
                      :disabled="loading"
                      aria-label="Remove row"
                      @click="removeReply(row.index)"
                    />
                  </div>
                </template>
              </UTable>
            </div>
            <small class="text-muted-color">
              Added to every Access-Accept. A switch uses the attributes it knows and ignores the
              rest. Turn off Send to keep a row without sending it. Comments are stored with the row
              and are not sent. A comment row has no attribute. Repeat a name to send it twice.
              <code>26.&lt;vendor&gt;.&lt;type&gt;</code> sends an attribute that is not in the
              built-in list. Remove every row and save to send none.
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
          <SearchInput v-model="clientSearch" class="mb-3 max-w-xs" />
          <UTable
            :data="filteredClients"
            :columns="clientColumns"
            :empty="
              clientSearch && clients.length
                ? 'Nothing matches the search.'
                : 'No RADIUS clients yet.'
            "
          >
            <template #actions-cell="{ row }">
              <UButton
                icon="i-lucide-pencil"
                variant="outline"
                color="neutral"
                size="sm"
                @click="editClient(row.original)"
              />
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
          <SearchInput v-model="policySearch" class="mb-3 max-w-xs" />
          <UTable
            :data="filteredPolicies"
            :columns="policyColumns"
            :empty="
              policySearch && policies.length ? 'Nothing matches the search.' : 'No group rules yet.'
            "
          >
            <template #actions-cell="{ row }">
              <UButton
                icon="i-lucide-pencil"
                variant="outline"
                color="neutral"
                size="sm"
                @click="editPolicy(row.original)"
              />
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
          <SearchInput v-model="eventSearch" class="mb-3 max-w-xs" />
          <UTable
            :data="filteredEvents"
            :columns="eventColumns"
            :empty="
              eventSearch && events.length ? 'Nothing matches the search.' : 'No login attempts yet.'
            "
          >
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
        <UFormField label="Name">
          <UInput id="client-name" v-model.trim="client.name" class="w-full" autofocus />
          <small v-if="clientSubmitted && !client.name?.trim()" class="text-red-500"
            >Name is required.</small
          >
        </UFormField>
        <UFormField label="Address">
          <UInput
            id="client-address"
            v-model.trim="client.address"
            placeholder="10.0.0.5"
            class="w-full"
          />
          <small v-if="clientSubmitted && !client.address?.trim()" class="text-red-500"
            >Address is required.</small
          >
        </UFormField>
        <UFormField label="Shared secret">
          <PasswordInput id="client-secret" v-model="client.secret" />
          <small v-if="clientSubmitted && !client.id && !client.secret" class="text-red-500"
            >Secret is required.</small
          >
          <small v-else-if="client.id" class="text-muted-color"
            >Leave blank to keep the current secret.</small
          >
        </UFormField>
        <UFormField label="Enabled">
          <USwitch v-model="client.enabled" id="client-enabled" />
        </UFormField>
      </div>
    </template>
    <template #footer="{ close }">
      <div class="flex w-full gap-2">
        <UButton
          v-if="client.id"
          label="Delete"
          icon="i-lucide-trash"
          color="error"
          variant="ghost"
          :loading="clientDeleting"
          @click="removeClient"
        />
        <UButton class="ms-auto" label="Cancel" icon="i-lucide-x" variant="ghost" @click="close" />
        <UButton label="Save" icon="i-lucide-check" :loading="clientSaving" @click="saveClient" />
      </div>
    </template>
  </FormModal>

  <FormModal v-model:open="policyDialog" :source="policy" title="LDAP group">
    <template #body>
      <div class="flex flex-col gap-6">
        <UFormField label="Group DN">
          <div class="flex gap-2">
            <UInput
              id="group-dn"
              v-model.trim="policy.group_dn"
              placeholder="CN=wdm-ops,OU=Groups,DC=example,DC=com"
              class="w-full"
              autofocus
            />
            <UButton
              icon="i-lucide-network"
              label="Browse"
              variant="outline"
              color="neutral"
              @click="browserVisible = true"
            />
          </div>
          <small v-if="policySubmitted && !policy.group_dn?.trim()" class="text-red-500"
            >Group DN is required.</small
          >
        </UFormField>
        <UFormField label="All devices" hint="Administrators who may log in to every role.">
          <USwitch v-model="policy.all_devices" id="all-devices" />
        </UFormField>
        <UFormField v-if="!policy.all_devices" label="Device roles">
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
        </UFormField>
      </div>
    </template>
    <template #footer="{ close }">
      <div class="flex w-full gap-2">
        <UButton
          v-if="policy.id"
          label="Delete"
          icon="i-lucide-trash"
          color="error"
          variant="ghost"
          :loading="policyDeleting"
          @click="removePolicy"
        />
        <UButton class="ms-auto" label="Cancel" icon="i-lucide-x" variant="ghost" @click="close" />
        <UButton label="Save" icon="i-lucide-check" :loading="policySaving" @click="savePolicy" />
      </div>
    </template>
  </FormModal>

  <LdapTreeBrowser v-model:visible="browserVisible" @select="onGroupDnSelected" />
</template>

<style>
.radius-reply-drop-line {
  position: fixed;
  top: 0;
  left: 0;
  z-index: 50;
  height: 3px;
  pointer-events: none;
  border-radius: 9999px;
  background: var(--ui-primary);
  box-shadow: 0 0 0 1px var(--ui-bg);
  opacity: 0;
  will-change: transform, width;
}
.radius-reply-drop-line::before {
  content: '';
  position: absolute;
  left: -5px;
  top: 50%;
  width: 11px;
  height: 11px;
  border-radius: 9999px;
  background: var(--ui-primary);
  box-shadow: 0 0 0 1px var(--ui-bg);
  transform: translateY(-50%);
}
.radius-reply-dragging > td,
.radius-reply-dragging > [data-slot='td'] {
  opacity: 0.4;
  background: color-mix(in oklab, var(--ui-primary) 8%, transparent) !important;
}
.radius-reply-dragging > td:first-child,
.radius-reply-dragging > [data-slot='td']:first-child {
  box-shadow: inset 2px 0 0 var(--ui-primary);
}
.radius-reply-table {
  overscroll-behavior: contain;
}
.radius-reply-table [data-slot='th'],
.radius-reply-table [data-slot='td'] {
  border-inline-end: 1px solid var(--ui-border-accented);
}
.radius-reply-table [data-slot='th']:last-child,
.radius-reply-table [data-slot='td']:last-child {
  border-inline-end: 0;
}
.radius-reply-table [data-slot='tbody'] [data-slot='tr']:not(:last-child) [data-slot='td'] {
  border-block-end: 1px solid var(--ui-border-accented);
}
.radius-reply-table [data-slot='thead'] [data-slot='th'] {
  position: sticky;
  top: 0;
  z-index: 10;
  background-color: var(--ui-bg-elevated);
  border-block-end: 1px solid var(--ui-border-accented);
}
.radius-reply-table [data-slot='td']:hover {
  background: color-mix(in oklab, var(--ui-bg-elevated) 70%, transparent);
}
.radius-reply-table [data-slot='td']:focus-within {
  position: relative;
  z-index: 1;
  background: var(--ui-bg);
  box-shadow: inset 0 0 0 2px var(--ui-primary);
}
</style>
