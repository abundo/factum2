<script setup>
import { useToast } from '@nuxt/ui/composables'
import { computed, ref, watch } from 'vue'
import { updateInterfaceVlans } from '@/api/devices'
import PasswordInput from '@/components/PasswordInput.vue'
import { useDeviceCredentials } from '@/composables/useDeviceCredentials'

const props = defineProps({
  // Full interface list for the open device - matrix rows.
  interfaces: { type: Array, default: () => [] },
  deviceId: { type: Number, default: null },
  // Lower-cased device platform, only used to disable Q-in-Q for vrp - its
  // driver has no dot1q-tunnel read/write support (see SetInterfaceVLANs'
  // doc comment in internal/drivers/driver_vrp.go).
  platform: { type: String, default: '' },
  deviceName: { type: String, default: '' },
})

const open = defineModel('open', { type: Boolean, default: false })

// Emitted after a save actually succeeds, so DeviceList.vue's already-open
// device/interfaces reloads - this dialog has no list of its own to keep in
// sync, same pattern as ServiceEditDialog.vue's 'saved'/'deleted'.
const emit = defineEmits(['saved'])

const toast = useToast()
const {
  credentialsDialog,
  promptUsername,
  promptPassword,
  withCredentials,
  submitCredentials,
  cancelCredentials,
  rememberSuccess,
  rememberFailure,
} = useDeviceCredentials()

// Cell states cycle: Untagged → Tagged → QinQ → Excluded → …
const STATES = ['untagged', 'tagged', 'qinq', 'excluded']

const STATE_META = {
  untagged: { label: 'Untagged', short: 'U', color: 'primary', variant: 'solid' },
  tagged: { label: 'Tagged', short: 'T', color: 'success', variant: 'solid' },
  qinq: { label: 'QinQ', short: 'Q', color: 'warning', variant: 'solid' },
  excluded: { label: 'Excluded', short: '—', color: 'neutral', variant: 'outline' },
}

const qinqSupported = computed(() => props.platform?.toLowerCase() !== 'vrp')

// Working copy: ifaceId -> Map-like object of vid -> state
const matrix = ref({})
// Ordered list of VLAN IDs (column headers)
const vlanIds = ref([])
// Snapshot of original payload per iface for dirty detection
const originalPayload = ref(new Map())

const saving = ref(false)
const addVlanInput = ref(null)

// Same split as DeviceInterfacePicker / isPhysicalInterfaceType - VLAN
// switchport config only applies to real ports (Ethernet/...), not
// virtual SVIs/subinterfaces or LAGs.
function isPhysicalInterface(iface) {
  const t = iface?.type ?? ''
  return t !== '' && t !== 'virtual' && t !== 'lag'
}

// L3 ports use "no switchport" on EOS/VRP - empty SwitchportMode means not
// a switchport, so they have no traditional VLAN membership to edit.
function isSwitchport(iface) {
  const mode = (iface?.switchport_mode ?? '').toLowerCase()
  return mode === 'access' || mode === 'trunk' || mode === 'dot1q-tunnel'
}

// Matrix rows: physical ports currently in switchport mode only.
const sortedInterfaces = computed(() =>
  [...(props.interfaces ?? [])]
    .filter((i) => isPhysicalInterface(i) && isSwitchport(i))
    .sort((a, b) => (a.name ?? '').localeCompare(b.name ?? '', undefined, { numeric: true })),
)

const hasChanges = computed(() => {
  for (const iface of sortedInterfaces.value) {
    const next = buildPayloadForIface(iface.id)
    const prev = originalPayload.value.get(iface.id)
    if (!prev) continue
    if (
      next.switchport_mode !== prev.switchport_mode ||
      next.untagged_vlan !== prev.untagged_vlan ||
      !sameIntList(next.tagged_vlans, prev.tagged_vlans)
    ) {
      return true
    }
  }
  return false
})

function sameIntList(a, b) {
  if (a.length !== b.length) return false
  const sa = [...a].sort((x, y) => x - y)
  const sb = [...b].sort((x, y) => x - y)
  return sa.every((v, i) => v === sb[i])
}

function cellState(iface, vid) {
  if (iface.switchport_mode === 'dot1q-tunnel' && iface.untagged_vlan === vid) return 'qinq'
  if (iface.untagged_vlan === vid) return 'untagged'
  if ((iface.tagged_vlans ?? []).includes(vid)) return 'tagged'
  return 'excluded'
}

function collectVlanIds(ifaces) {
  const set = new Set()
  for (const iface of ifaces) {
    // Only VIDs from switchport ports feed the column set - L3/"no
    // switchport" interfaces don't own traditional VLAN membership.
    if (!isSwitchport(iface)) continue
    if (iface.untagged_vlan) set.add(iface.untagged_vlan)
    for (const vid of iface.tagged_vlans ?? []) {
      if (vid) set.add(vid)
    }
  }
  return [...set].sort((a, b) => a - b)
}

function snapshotFromInterfaces() {
  // Same filter as the matrix rows - physical switchports only.
  const ifaces = sortedInterfaces.value
  const vids = collectVlanIds(ifaces)
  vlanIds.value = vids

  const next = {}
  const originals = new Map()
  for (const iface of ifaces) {
    const row = {}
    for (const vid of vids) {
      row[vid] = cellState(iface, vid)
    }
    next[iface.id] = row
    originals.set(iface.id, {
      switchport_mode: iface.switchport_mode || '',
      untagged_vlan: iface.untagged_vlan || 0,
      tagged_vlans: [...(iface.tagged_vlans ?? [])],
    })
  }
  matrix.value = next
  originalPayload.value = originals
}

watch(open, (isOpen) => {
  if (!isOpen) return
  addVlanInput.value = null
  snapshotFromInterfaces()
})

function getState(ifaceId, vid) {
  return matrix.value[ifaceId]?.[vid] ?? 'excluded'
}

function setState(ifaceId, vid, state) {
  // Replace row + matrix root so Vue always sees a new reference for the
  // cell that just changed (nested in-place mutation is easy to miss in
  // the matrix re-render path).
  const row = { ...(matrix.value[ifaceId] ?? {}), [vid]: state }
  matrix.value = { ...matrix.value, [ifaceId]: row }
}

function cycleCell(ifaceId, vid) {
  const current = getState(ifaceId, vid)
  let idx = STATES.indexOf(current)
  if (idx < 0) idx = STATES.length - 1
  let next = STATES[(idx + 1) % STATES.length]
  // VRP has no Q-in-Q support - skip that state in the cycle.
  if (next === 'qinq' && !qinqSupported.value) {
    next = STATES[(idx + 2) % STATES.length]
  }

  applyState(ifaceId, vid, next)
}

function applyState(ifaceId, vid, next) {
  const row = matrix.value[ifaceId] ?? {}
  const vids = vlanIds.value

  if (next === 'untagged') {
    // Only one untagged (or QinQ outer) VLAN per interface.
    for (const v of vids) {
      const s = row[v]
      if (s === 'untagged' || s === 'qinq') setState(ifaceId, v, 'excluded')
    }
    setState(ifaceId, vid, 'untagged')
    return
  }

  if (next === 'tagged') {
    // QinQ is exclusive - drop any QinQ assignment on this interface.
    for (const v of vids) {
      if (row[v] === 'qinq') setState(ifaceId, v, 'excluded')
    }
    setState(ifaceId, vid, 'tagged')
    return
  }

  if (next === 'qinq') {
    // QinQ owns the whole interface - clear every other assignment.
    for (const v of vids) {
      setState(ifaceId, v, 'excluded')
    }
    setState(ifaceId, vid, 'qinq')
    return
  }

  // excluded
  setState(ifaceId, vid, 'excluded')
}

function buildPayloadForIface(ifaceId) {
  const row = matrix.value[ifaceId] ?? {}
  let untagged = 0
  let mode = ''
  const tagged = []

  for (const vid of vlanIds.value) {
    const state = row[vid] ?? 'excluded'
    if (state === 'qinq') {
      return {
        switchport_mode: 'dot1q-tunnel',
        untagged_vlan: Number(vid),
        tagged_vlans: [],
      }
    }
    if (state === 'untagged') {
      untagged = Number(vid)
    } else if (state === 'tagged') {
      tagged.push(Number(vid))
    }
  }

  if (tagged.length > 0) {
    mode = 'trunk'
  } else if (untagged) {
    mode = 'access'
  }

  return {
    switchport_mode: mode,
    untagged_vlan: untagged,
    tagged_vlans: tagged,
  }
}

function addVlanColumn() {
  const vid = Number(addVlanInput.value)
  if (!Number.isInteger(vid) || vid < 1 || vid > 4094) {
    toast.add({
      color: 'error',
      title: 'Invalid VLAN',
      description: 'Enter a VLAN ID between 1 and 4094.',
      duration: 3000,
    })
    return
  }
  if (vlanIds.value.includes(vid)) {
    toast.add({
      color: 'info',
      title: 'VLAN already listed',
      description: `VLAN ${vid} is already a column.`,
      duration: 2500,
    })
    addVlanInput.value = null
    return
  }
  vlanIds.value = [...vlanIds.value, vid].sort((a, b) => a - b)
  for (const iface of sortedInterfaces.value) {
    if (!matrix.value[iface.id]) matrix.value[iface.id] = {}
    if (matrix.value[iface.id][vid] == null) {
      matrix.value[iface.id][vid] = 'excluded'
    }
  }
  addVlanInput.value = null
}

function doSave(username, password) {
  if (!props.deviceId) return

  const payload = []
  for (const iface of sortedInterfaces.value) {
    const next = buildPayloadForIface(iface.id)
    const prev = originalPayload.value.get(iface.id) ?? {
      switchport_mode: '',
      untagged_vlan: 0,
      tagged_vlans: [],
    }
    if (
      next.switchport_mode !== prev.switchport_mode ||
      next.untagged_vlan !== prev.untagged_vlan ||
      !sameIntList(next.tagged_vlans, prev.tagged_vlans)
    ) {
      payload.push({
        id: iface.id,
        switchport_mode: next.switchport_mode,
        untagged_vlan: next.untagged_vlan,
        tagged_vlans: next.tagged_vlans,
      })
    }
  }

  if (payload.length === 0) {
    toast.add({
      color: 'info',
      title: 'Nothing to save',
      description: 'No VLAN assignments were changed.',
      duration: 3000,
    })
    return
  }

  const deviceId = props.deviceId
  saving.value = true
  updateInterfaceVlans(deviceId, username, password, payload)
    .then((data) => {
      rememberSuccess(deviceId, username, password)
      toast.add({
        color: 'success',
        title: 'VLANs updated',
        description: 'Changes were saved to Netbox and factum cache refreshed.',
        duration: 3000,
      })
      open.value = false
      // Pass the post-sync device payload so DeviceList can refresh without
      // a second GET (the API already re-pulled interfaces/VLANs via SyncDB).
      emit('saved', data)
    })
    .catch((err) => {
      rememberFailure(deviceId, username, password)
      toast.add({
        color: 'error',
        title: 'Update failed',
        description: err?.response?.data?.error ?? 'Failed to update VLANs.',
        duration: 4000,
      })
    })
    .finally(() => {
      saving.value = false
    })
}

function save() {
  if (!hasChanges.value) {
    toast.add({
      color: 'info',
      title: 'Nothing to save',
      description: 'No VLAN assignments were changed.',
      duration: 3000,
    })
    return
  }
  withCredentials(props.deviceId, doSave)
}

function metaFor(state) {
  return STATE_META[state] ?? STATE_META.excluded
}
</script>

<template>
  <UModal
    v-model:open="open"
    :title="deviceName ? `VLANs — ${deviceName}` : 'VLANs'"
    :ui="{
      content: 'w-[90vw] h-[90vh] sm:max-w-none flex flex-col',
      body: 'flex-1 min-h-0 overflow-hidden',
    }"
  >
    <template #body>
      <div class="flex flex-col h-full min-h-0 gap-3">
        <div class="flex flex-wrap items-end gap-2 shrink-0">
          <div class="flex flex-col gap-1">
            <label for="add-vlan" class="text-sm text-muted-color">Add VLAN column</label>
            <div class="flex gap-2 items-center">
              <UInputNumber
                id="add-vlan"
                v-model="addVlanInput"
                :min="1"
                :max="4094"
                placeholder="VID"
                size="sm"
                class="w-28"
                @keyup.enter="addVlanColumn"
              />
              <UButton
                label="Add"
                icon="i-lucide-plus"
                size="sm"
                variant="outline"
                color="neutral"
                @click="addVlanColumn"
              />
            </div>
          </div>
          <div class="flex flex-wrap gap-3 text-xs text-muted-color ml-auto items-center">
            <span v-for="key in STATES" :key="key" class="inline-flex items-center gap-1">
              <UBadge
                :label="metaFor(key).short"
                :color="metaFor(key).color"
                :variant="metaFor(key).variant"
                size="sm"
              />
              {{ metaFor(key).label }}
            </span>
            <span class="text-muted-color">Click a cell to cycle</span>
          </div>
        </div>

        <div v-if="sortedInterfaces.length === 0" class="text-sm text-muted-color p-4">
          No physical switchport interfaces on this device. L3 ports (no switchport) and virtual
          interfaces are not listed here.
        </div>

        <div v-else-if="vlanIds.length === 0" class="text-sm text-muted-color p-4">
          No VLANs assigned on switchport interfaces yet. Use “Add VLAN column” to start editing.
        </div>

        <div v-else class="flex-1 min-h-0 overflow-auto border border-default rounded-md">
          <table class="border-collapse text-sm w-max min-w-full">
            <thead class="sticky top-0 z-20 bg-default">
              <tr>
                <th
                  class="sticky left-0 z-30 bg-default border-b border-r border-default px-3 py-2 text-left font-semibold whitespace-nowrap"
                >
                  Interface
                </th>
                <th
                  v-for="vid in vlanIds"
                  :key="vid"
                  class="border-b border-default px-2 py-2 text-center font-semibold whitespace-nowrap min-w-[5.5rem]"
                >
                  {{ vid }}
                </th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="iface in sortedInterfaces" :key="iface.id" class="group">
                <td
                  class="sticky left-0 z-10 bg-default group-hover:bg-elevated border-b border-r border-default px-3 py-1.5 whitespace-nowrap font-medium"
                >
                  {{ iface.name }}
                </td>
                <td
                  v-for="vid in vlanIds"
                  :key="vid"
                  class="border-b border-default px-1 py-1 text-center group-hover:bg-elevated/50"
                >
                  <UButton
                    :label="metaFor(getState(iface.id, vid)).short"
                    :color="metaFor(getState(iface.id, vid)).color"
                    :variant="metaFor(getState(iface.id, vid)).variant"
                    size="xs"
                    class="min-w-[2.5rem] justify-center"
                    :title="metaFor(getState(iface.id, vid)).label"
                    @click="cycleCell(iface.id, vid)"
                  />
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>
    </template>

    <template #footer>
      <UButton label="Cancel" icon="i-lucide-x" variant="ghost" @click="open = false" />
      <UButton
        label="Save"
        icon="i-lucide-check"
        :loading="saving"
        :disabled="!hasChanges"
        @click="save"
      />
    </template>
  </UModal>

  <UModal
    v-model:open="credentialsDialog"
    title="Device credentials"
    :ui="{ content: 'sm:max-w-sm' }"
    @update:open="(isOpen) => !isOpen && cancelCredentials()"
  >
    <template #body>
      <div class="flex flex-col gap-3">
        <div class="flex flex-col gap-1">
          <label for="vlan-prompt-username" class="text-sm text-muted-color">Username</label>
          <UInput
            id="vlan-prompt-username"
            v-model="promptUsername"
            autocomplete="off"
            autofocus
            class="w-full"
            @keyup.enter="submitCredentials"
          />
        </div>
        <div class="flex flex-col gap-1">
          <label for="vlan-prompt-password" class="text-sm text-muted-color">Password</label>
          <PasswordInput
            id="vlan-prompt-password"
            v-model="promptPassword"
            autocomplete="new-password"
            @keyup.enter="submitCredentials"
          />
        </div>
      </div>
    </template>

    <template #footer>
      <UButton label="Cancel" icon="i-lucide-x" variant="ghost" @click="cancelCredentials" />
      <UButton
        label="Continue"
        icon="i-lucide-check"
        :disabled="!promptUsername || !promptPassword"
        @click="submitCredentials"
      />
    </template>
  </UModal>
</template>
