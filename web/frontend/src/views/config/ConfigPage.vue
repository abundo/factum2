<script setup>
import { useToast } from '@nuxt/ui/composables'
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import {
  createMacro,
  createScope,
  createServiceType,
  createVariable,
  deleteAssignment,
  deleteMacro,
  deleteScope,
  deleteServiceType,
  deleteVariable,
  detachScope,
  moveScope,
  getMatrix,
  listAssignments,
  listScopes,
  listMacros,
  listServiceTypes,
  listVariables,
  renderConfig,
  resolveInterface,
  updateMacro,
  updateServiceType,
  updateVariable,
  upsertAssignment,
} from '@/api/config'
import { getCustomers } from '@/api/customers'
import { getDevices } from '@/api/devices'
import { getServiceEndpoints, getServices, putServiceEndpoints } from '@/api/services'
import ConfigNodeInspector from '@/components/ConfigNodeInspector.vue'
import ConfigScopeTree from '@/components/ConfigScopeTree.vue'
import GoTemplateEditor from '@/components/GoTemplateEditor.vue'
import SearchInput from '@/components/SearchInput.vue'
import { useAuthStore } from '@/stores/auth'
import { cfgmgmtMacroSchema, withCfgmgmtContext } from '@/utils/goTemplateSchemas'

defineOptions({ name: 'ConfigPage' })

const toast = useToast()
const authStore = useAuthStore()
const treeRef = ref(null)
const filter = ref('')
const saving = ref(false)
const tab = ref('tree')
const catalogOpen = ref(false)
const catalogTab = ref('variables')
const selected = ref(null)
const reloadKey = ref(0)

const menu = ref({ open: false, x: 0, y: 0, node: null })
const dialog = ref(null)
const form = ref({})
const formError = ref('')
const confirm = ref(null)
const attachDeviceId = ref(null)
const attachServiceId = ref(null)
const devices = ref([])
const customers = ref([])
const attachableServices = ref([])
const draftEndpoint = ref(null)
const scopesById = ref({})

const assignments = ref([])
const variables = ref([])
const resolved = ref([])
const matrixVar = ref(null)
const matrixRows = ref([])
const serviceTypes = ref([])
const macros = ref([])
const previewDeviceId = ref(null)
const preview = ref(null)
const macroBodyPlaceholder = 'Go text/template. Inserted with {{include "name"}}.'

const tabItems = [
  {
    label: 'Tree',
    value: 'tree',
    slot: 'tree',
    class: 'flex min-h-0 flex-1 flex-col overflow-auto lg:overflow-hidden',
  },
  { label: 'Matrix', value: 'matrix', slot: 'matrix' },
]
const catalogTabItems = [
  { label: 'Variables', value: 'variables', slot: 'variables' },
  { label: 'Service types', value: 'types', slot: 'types' },
  { label: 'Macros', value: 'macros', slot: 'macros' },
]

const varTypeOptions = [
  { label: 'string', value: 'string' },
  { label: 'int', value: 'int' },
  { label: 'bool', value: 'bool' },
  { label: 'enum', value: 'enum' },
  { label: 'ip', value: 'ip' },
  { label: 'prefix', value: 'prefix' },
  { label: 'vlan', value: 'vlan' },
  { label: 'interface_ref', value: 'interface_ref' },
  { label: 'secret', value: 'secret' },
  { label: 'list (array)', value: 'list' },
  { label: 'map (hash)', value: 'map' },
]

const platformOptions = [
  { label: 'eos', value: 'eos' },
  { label: 'ios-xr', value: 'ios-xr' },
  { label: 'sros', value: 'sros' },
  { label: 'sros-md', value: 'sros-md' },
  { label: 'vrp', value: 'vrp' },
]

const syncSourceOptions = [
  { label: '(none)', value: '' },
  { label: 'eline — DeviceConfig.ELINEs', value: 'eline' },
  { label: 'elan — DeviceConfig.ELANs', value: 'elan' },
  { label: 'l3vpn — DeviceConfig.L3VPNs', value: 'l3vpn' },
]
const netboxTypeOptions = [
  { label: '(none)', value: '' },
  { label: 'evpl — L2VPN point to point', value: 'evpl' },
  { label: 'vpls — L2VPN multipoint', value: 'vpls' },
  { label: 'vrf — IPAM VRF', value: 'vrf' },
]

const deviceOptions = computed(() => devices.value.map((d) => ({ label: d.name, value: d.id })))
const customerOptions = computed(() =>
  customers.value.map((c) => ({ label: c.name, value: c.id })),
)
const attachServiceOptions = computed(() =>
  attachableServices.value.map((s) => ({
    label: `${s.service_id} (${s.service_type || 'typed'})`,
    value: s.id,
  })),
)
const capacityTypeOptions = computed(() =>
  serviceTypes.value.map((t) => ({ label: t.name, value: t.name })),
)
const varOptions = computed(() => variables.value.map((v) => ({ label: v.name, value: v.name })))
const categoryOptions = [
  { label: 'CN', value: 'CN' },
  { label: 'CI', value: 'CI' },
]
const orgKindTitles = { folder: 'Folder', site: 'Site', location: 'Location' }

const macroSchema = computed(() =>
  withCfgmgmtContext(cfgmgmtMacroSchema, {
    macros: macros.value,
    variables: variables.value,
  }),
)
const assignDef = computed(() => {
  const id = optionValue(form.value?.variable_def_id)
  return variables.value.find((v) => v.id === id) ?? null
})
const assignEnumOptions = computed(() => {
  const en = assignDef.value?.constraints?.enum
  if (!Array.isArray(en)) return []
  return en.map((v) => ({ label: String(v), value: v }))
})
const matrixStart = computed(() => nearestMatrixNode(selected.value) || selected.value)

const constraintsPlaceholder = computed(() => {
  switch (optionValue(form.value?.type)) {
    case 'list':
      return '{"items":{"type":"ip"}}'
    case 'map':
      return '{"keys":{"type":"string"},"values":{"type":"int"}}'
    case 'enum':
      return '{"enum":["a","b"]}'
    case 'int':
      return '{"min":0,"max":100}'
    case 'string':
      return '{"regex":"^\\\\S+$"}'
    default:
      return '{"enum":["a","b"]}'
  }
})

const constraintsHint = computed(() => {
  switch (optionValue(form.value?.type)) {
    case 'list':
      return 'Type list entries with items. Shorthand {"items":"ip"} or {"items":{"type":"int","min":1}}. min/max are list length.'
    case 'map':
      return 'Type map keys and values with keys and values. Example {"keys":"string","values":{"type":"int"}}. JSON keys are strings; keys may be string, int, enum, ip, prefix, vlan. min/max are size.'
    case 'enum':
      return 'Allowed values: {"enum":["a","b"]}.'
    default:
      return 'Optional. Enum, min, max, regex depending on type.'
  }
})

function errMsg(err, fallback) {
  return err.response?.data?.error ?? fallback
}

function applyFilter(q) {
  treeRef.value?.filter(q)
}

function isOrgKind(kind) {
  return kind === 'folder' || kind === 'site' || kind === 'location'
}

function isReservedFolder(node) {
  if (!node || node.kind !== 'folder') return false
  if (node.title === 'global' && !node.parent_id) return true
  return node.title === '_catalog' || node.title === '_services'
}

function canAddParameter(kind) {
  return isOrgKind(kind) || kind === 'device' || kind === 'interface' || kind === 'service'
}

function itemsFor(node) {
  const write = authStore.canWrite
  if (!write) {
    return node
      ? [
          { id: 'expand', label: 'Expand' },
          { id: 'collapse', label: 'Collapse' },
        ]
      : []
  }
  if (!node) {
    return [{ id: 'add-folder', label: 'Add folder' }]
  }
  const items = [
    { id: 'expand', label: 'Expand' },
    { id: 'collapse', label: 'Collapse' },
  ]
  if (isOrgKind(node.kind)) {
    items.push({ id: 'sep' }, { id: 'add-folder', label: 'Add child folder' })
    items.push({ id: 'add-site', label: 'Add site' })
    items.push({ id: 'add-location', label: 'Add location' })
    items.push({ id: 'attach-device', label: 'Attach device' })
    items.push({ id: 'add-parameter', label: 'Add parameter object' })
    items.push({ id: 'add-cli', label: 'Add CLI object' })
    items.push({ id: 'create-service', label: 'Create service' })
    items.push({ id: 'attach-service', label: 'Attach existing service' })
  }
  if (node.kind === 'device' || node.kind === 'interface') {
    items.push({ id: 'sep-cli' }, { id: 'add-parameter', label: 'Add parameter object' })
    items.push({ id: 'add-cli', label: 'Add CLI object' })
    items.push({ id: 'create-service', label: 'Create service' })
  }
  if (node.kind === 'service') {
    items.push({ id: 'sep-param' }, { id: 'add-parameter', label: 'Add parameter object' })
  }
  if (node.kind === 'device') {
    items.push({ id: 'attach-service', label: 'Attach existing service' })
    items.push({ id: 'sep2' }, { id: 'detach', label: 'Detach', danger: true })
  } else if (node.kind === 'service_ref') {
    items.push({ id: 'sep-ref' }, { id: 'open-service', label: 'Open service' })
    items.push({ id: 'remove-endpoint', label: 'Remove endpoint', danger: true })
  } else if (node.kind === 'service') {
    items.push({ id: 'sep2' }, { id: 'del', label: 'Detach from tree', danger: true })
  } else if (
    node.kind !== 'interface' &&
    node.kind !== 'service_endpoint' &&
    !isReservedFolder(node)
  ) {
    items.push({ id: 'sep2' }, { id: 'del', label: 'Delete', danger: true })
  }
  return items
}

const menuItems = computed(() => itemsFor(menu.value.node))

function onContextMenu({ x, y, node }) {
  menu.value = { open: true, x, y, node }
}

function closeMenu() {
  menu.value = { ...menu.value, open: false }
}

function isLeafKind(kind) {
  return (
    kind === 'parameter' ||
    kind === 'cli' ||
    kind === 'service' ||
    kind === 'service_endpoint' ||
    kind === 'service_ref'
  )
}

function servicesFolderId() {
  const rows = Object.values(scopesById.value)
  const root = rows.find((s) => s.kind === 'folder' && s.name === 'global' && !s.parent_id)
  if (!root) return null
  const folder = rows.find(
    (s) => s.kind === 'folder' && s.name === '_services' && s.parent_id === root.id,
  )
  return folder?.id ?? null
}

function serviceParentId(node) {
  if (!node) return servicesFolderId()
  if (isOrgKind(node.kind)) return node.id
  if (node.kind === 'device') {
    const p = scopesById.value[node.parent_id]
    if (p && isOrgKind(p.kind)) return p.id
    return servicesFolderId()
  }
  if (node.kind === 'interface') {
    return serviceParentId(scopesById.value[node.parent_id])
  }
  return servicesFolderId()
}

function firstRoleForType(typeName) {
  const st = serviceTypes.value.find((t) => t.name === typeName)
  return st?.endpoint_roles?.[0]?.name || 'endpoint'
}

function nearestMatrixNode(node) {
  if (!node) return null
  if (!isLeafKind(node.kind)) return node
  let cur = node
  const map = scopesById.value
  while (cur) {
    if (
      cur.kind === 'folder' ||
      cur.kind === 'site' ||
      cur.kind === 'location' ||
      cur.kind === 'device'
    ) {
      return cur
    }
    if (!cur.parent_id) return null
    cur = map[cur.parent_id]
  }
  return null
}

function onSelect(node) {
  selected.value = node
  if (node?.id) loadNodeDetails(node)
  if (node?.kind === 'device' && node.device_id) {
    previewDeviceId.value = node.device_id
  } else if (isLeafKind(node?.kind)) {
    const anc = nearestMatrixNode(node)
    if (anc?.kind === 'device' && anc.device_id) {
      previewDeviceId.value = anc.device_id
    }
  }
}

async function loadNodeDetails(node) {
  try {
    assignments.value = await listAssignments(node.id)
  } catch {
    assignments.value = []
  }
  resolved.value = []
  if (node.kind === 'interface' && node.interface_id) {
    try {
      resolved.value = await resolveInterface(node.interface_id)
    } catch {
      resolved.value = []
    }
  }
}

async function runMenu(id) {
  const node = menu.value.node
  closeMenu()
  if (id === 'expand') {
    treeRef.value?.expandNode(node?.key)
    return
  }
  if (id === 'collapse') {
    treeRef.value?.collapseNode(node?.key)
    return
  }
  if (id === 'add-folder' || id === 'add-site' || id === 'add-location') {
    const kind = id === 'add-site' ? 'site' : id === 'add-location' ? 'location' : 'folder'
    form.value = { parent_id: node?.id, name: '', kind }
    dialog.value = 'folder'
    return
  }
  if (id === 'add-parameter' && canAddParameter(node?.kind)) {
    form.value = { parent_id: node?.id, name: '', kind: 'parameter' }
    dialog.value = 'parameter'
    return
  }
  if (id === 'attach-device') {
    form.value = { parent_id: node?.id }
    attachDeviceId.value = null
    if (!devices.value.length) {
      try {
        devices.value = await getDevices()
      } catch {
        devices.value = []
      }
    }
    dialog.value = 'device'
    return
  }
  if (id === 'add-cli') {
    form.value = { parent_id: node?.id, name: '', kind: 'cli', platform: 'eos' }
    dialog.value = 'cli'
    return
  }
  if (id === 'create-service') {
    form.value = {
      parent_id: serviceParentId(node),
      category: 'CN',
      service_type: serviceTypes.value[0]?.name || 'ELINE',
      company: null,
      from_interface: node?.kind === 'interface' ? node : null,
    }
    if (!customers.value.length) {
      getCustomers()
        .then((rows) => {
          customers.value = rows ?? []
        })
        .catch(() => {
          customers.value = []
        })
    }
    dialog.value = 'create-service'
    return
  }
  if (id === 'attach-service') {
    form.value = { parent_id: serviceParentId(node) }
    attachServiceId.value = null
    loadAttachableServices()
    dialog.value = 'attach-service'
    return
  }
  if (id === 'open-service' && node?.canonical_id) {
    selected.value = {
      key: String(node.canonical_id),
      id: node.canonical_id,
      title: node.service_label || node.title,
      kind: 'service',
      service_id: node.service_row_id,
    }
    loadNodeDetails(selected.value)
    return
  }
  if (id === 'remove-endpoint' && node) {
    confirm.value = { kind: 'remove-endpoint', node, label: node.title }
    return
  }
  if (id === 'del' && node?.id) {
    confirm.value = {
      kind: node.kind === 'service' ? 'detach-service' : 'scope',
      id: node.id,
      label: node.title,
    }
  }
  if (id === 'detach' && node?.id) {
    confirm.value = { kind: 'detach', id: node.id, label: node.title }
  }
}

async function loadAttachableServices() {
  try {
    const rows = await getServices()
    const attached = new Set()
    for (const s of Object.values(scopesById.value)) {
      if (s.kind === 'service' && s.service_id) attached.add(s.service_id)
    }
    attachableServices.value = (rows ?? []).filter((s) => {
      if (!s.service_type) return false
      const cat = (s.service_id || '').slice(0, 2)
      if (cat === 'VL' || cat === 'VI' || cat === 'LF' || cat === 'LI') return false
      return !attached.has(s.id)
    })
  } catch {
    attachableServices.value = []
  }
}

// endpointDisc matches cfgmgmt.endpointDisc: VLAN if present and != 0,
// else sha256 of canonical JSON fields (empty → "0").
function canonicalJSON(v) {
  if (v === null || v === undefined) return 'null'
  if (typeof v === 'boolean') return v ? 'true' : 'false'
  if (typeof v === 'number') return Number.isFinite(v) ? JSON.stringify(v) : 'null'
  if (typeof v === 'string') {
    return JSON.stringify(v)
      .replace(/&/g, '\\u0026')
      .replace(/</g, '\\u003c')
      .replace(/>/g, '\\u003e')
  }
  if (Array.isArray(v)) return `[${v.map(canonicalJSON).join(',')}]`
  if (typeof v === 'object') {
    const keys = Object.keys(v).sort()
    return `{${keys.map((k) => `${JSON.stringify(k)}:${canonicalJSON(v[k])}`).join(',')}}`
  }
  return 'null'
}

function sha256hexSync(message) {
  const bytes = new TextEncoder().encode(message)
  const K = [
    0x428a2f98, 0x71374491, 0xb5c0fbcf, 0xe9b5dba5, 0x3956c25b, 0x59f111f1, 0x923f82a4,
    0xab1c5ed5, 0xd807aa98, 0x12835b01, 0x243185be, 0x550c7dc3, 0x72be5d74, 0x80deb1fe,
    0x9bdc06a7, 0xc19bf174, 0xe49b69c1, 0xefbe4786, 0x0fc19dc6, 0x240ca1cc, 0x2de92c6f,
    0x4a7484aa, 0x5cb0a9dc, 0x76f988da, 0x983e5152, 0xa831c66d, 0xb00327c8, 0xbf597fc7,
    0xc6e00bf3, 0xd5a79147, 0x06ca6351, 0x14292967, 0x27b70a85, 0x2e1b2138, 0x4d2c6dfc,
    0x53380d13, 0x650a7354, 0x766a0abb, 0x81c2c92e, 0x92722c85, 0xa2bfe8a1, 0xa81a664b,
    0xc24b8b70, 0xc76c51a3, 0xd192e819, 0xd6990624, 0xf40e3585, 0x106aa070, 0x19a4c116,
    0x1e376c08, 0x2748774c, 0x34b0bcb5, 0x391c0cb3, 0x4ed8aa4a, 0x5b9cca4f, 0x682e6ff3,
    0x748f82ee, 0x78a5636f, 0x84c87814, 0x8cc70208, 0x90befffa, 0xa4506ceb, 0xbef9a3f7,
    0xc67178f2,
  ]
  const rotr = (n, x) => (x >>> n) | (x << (32 - n))
  let l = bytes.length
  const withBit = new Uint8Array(((l + 9 + 63) >> 6) << 6)
  withBit.set(bytes)
  withBit[l] = 0x80
  const view = new DataView(withBit.buffer)
  view.setUint32(withBit.length - 4, l * 8, false)
  let h0 = 0x6a09e667
  let h1 = 0xbb67ae85
  let h2 = 0x3c6ef372
  let h3 = 0xa54ff53a
  let h4 = 0x510e527f
  let h5 = 0x9b05688c
  let h6 = 0x1f83d9ab
  let h7 = 0x5be0cd19
  const w = new Uint32Array(64)
  for (let i = 0; i < withBit.length; i += 64) {
    for (let t = 0; t < 16; t++) w[t] = view.getUint32(i + t * 4, false)
    for (let t = 16; t < 64; t++) {
      const s0 = rotr(7, w[t - 15]) ^ rotr(18, w[t - 15]) ^ (w[t - 15] >>> 3)
      const s1 = rotr(17, w[t - 2]) ^ rotr(19, w[t - 2]) ^ (w[t - 2] >>> 10)
      w[t] = (w[t - 16] + s0 + w[t - 7] + s1) >>> 0
    }
    let a = h0
    let b = h1
    let c = h2
    let d = h3
    let e = h4
    let f = h5
    let g = h6
    let h = h7
    for (let t = 0; t < 64; t++) {
      const S1 = rotr(6, e) ^ rotr(11, e) ^ rotr(25, e)
      const ch = (e & f) ^ (~e & g)
      const t1 = (h + S1 + ch + K[t] + w[t]) >>> 0
      const S0 = rotr(2, a) ^ rotr(13, a) ^ rotr(22, a)
      const maj = (a & b) ^ (a & c) ^ (b & c)
      const t2 = (S0 + maj) >>> 0
      h = g
      g = f
      f = e
      e = (d + t1) >>> 0
      d = c
      c = b
      b = a
      a = (t1 + t2) >>> 0
    }
    h0 = (h0 + a) >>> 0
    h1 = (h1 + b) >>> 0
    h2 = (h2 + c) >>> 0
    h3 = (h3 + d) >>> 0
    h4 = (h4 + e) >>> 0
    h5 = (h5 + f) >>> 0
    h6 = (h6 + g) >>> 0
    h7 = (h7 + h) >>> 0
  }
  return [h0, h1, h2, h3, h4, h5, h6, h7]
    .map((n) => n.toString(16).padStart(8, '0'))
    .join('')
}

function endpointDisc(fields) {
  const m = fields && typeof fields === 'object' && !Array.isArray(fields) ? fields : {}
  if (Object.prototype.hasOwnProperty.call(m, 'vlan')) {
    const n = Number(m.vlan)
    if (Number.isFinite(n) && n !== 0) return String(Math.trunc(n))
  }
  const keys = Object.keys(m)
  if (!keys.length) return '0'
  const canon = canonicalJSON(m)
  if (!canon || canon === '{}') return '0'
  return sha256hexSync(canon)
}

function endpointIdentityOf(ep, serviceId) {
  return `${serviceId}:${ep.role}:${ep.device_id}:${ep.interface_id}:${endpointDisc(ep.fields)}`
}

function endpointMatchesRef(ep, node) {
  const sid = node.service_row_id ?? node.service_id
  const ident = endpointIdentityOf(ep, sid)
  if (node.identity) return node.identity === ident
  if (node.key && String(node.key).startsWith('ref:')) return node.key === `ref:${ident}`
  return (
    ep.role === node.role &&
    Number(ep.device_id) === Number(node.device_id) &&
    Number(ep.interface_id) === Number(node.interface_id) &&
    endpointDisc(ep.fields) === String(node.disc ?? '0')
  )
}

function onRebind({ ref, target }) {
  if (!ref?.service_row_id || !target?.interface_id || !target?.device_id) return
  getServiceEndpoints(ref.service_row_id)
    .then((rows) => {
      const next = (rows ?? []).map((ep) => {
        if (!endpointMatchesRef(ep, ref)) return ep
        return {
          role: ep.role,
          device_id: target.device_id,
          interface_id: target.interface_id,
          fields: ep.fields || {},
        }
      })
      return putServiceEndpoints(ref.service_row_id, {
        endpoints: next.map((ep) => ({
          role: ep.role,
          device_id: ep.device_id,
          interface_id: ep.interface_id,
          fields: ep.fields || {},
        })),
      })
    })
    .then(() => {
      reloadKey.value += 1
      loadScopesIndex()
    })
    .catch((err) =>
      toast.add({ color: 'error', title: 'Error', description: errMsg(err, 'Rebind failed.') }),
    )
}

function onMove({ id, parent_id, sort_order }) {
  const body = { parent_id }
  if (sort_order != null) body.sort_order = sort_order
  moveScope(id, body)
    .then(() => {
      reloadKey.value += 1
      loadScopesIndex()
    })
    .catch((err) =>
      toast.add({ color: 'error', title: 'Error', description: errMsg(err, 'Move failed.') }),
    )
}

function saveDialog() {
  saving.value = true
  let req
  if (dialog.value === 'folder') {
    const name = (form.value.name ?? '').trim()
    if (!name) {
      saving.value = false
      return
    }
    req = createScope({
      parent_id: form.value.parent_id,
      name,
      kind: form.value.kind || 'folder',
    })
  } else if (dialog.value === 'parameter') {
    const name = (form.value.name ?? '').trim()
    if (!name) {
      saving.value = false
      return
    }
    req = createScope({
      parent_id: form.value.parent_id,
      name,
      kind: 'parameter',
    })
  } else if (dialog.value === 'device') {
    if (!attachDeviceId.value) {
      saving.value = false
      return
    }
    req = createScope({
      parent_id: form.value.parent_id,
      kind: 'device',
      device_id: attachDeviceId.value,
    })
  } else if (dialog.value === 'cli') {
    const name = (form.value.name ?? '').trim()
    if (!name) {
      saving.value = false
      return
    }
    req = createScope({
      parent_id: form.value.parent_id,
      name,
      kind: 'cli',
      platform: optionValue(form.value.platform) ?? '',
      payload_kind: 'cli',
    })
  } else if (dialog.value === 'create-service') {
    const category = optionValue(form.value.category) || 'CN'
    const serviceType = optionValue(form.value.service_type)
    const company = optionValue(form.value.company)
    if (!serviceType || !company) {
      saving.value = false
      return
    }
    const fromIface = form.value.from_interface
    req = createScope({
      parent_id: form.value.parent_id,
      kind: 'service',
      attach: {
        category,
        service_type: serviceType,
        company,
      },
    }).then((node) => {
      if (fromIface?.device_id && fromIface?.interface_id) {
        draftEndpoint.value = {
          role: firstRoleForType(serviceType),
          device_id: fromIface.device_id,
          interface_id: fromIface.interface_id,
          fields: {},
          service_id: node.service_id,
        }
      }
      selected.value = {
        key: String(node.id),
        id: node.id,
        title: node.name,
        kind: 'service',
        service_id: node.service_id,
      }
      return node
    })
  } else if (dialog.value === 'attach-service') {
    if (!attachServiceId.value) {
      saving.value = false
      return
    }
    req = createScope({
      parent_id: form.value.parent_id,
      kind: 'service',
      service_id: attachServiceId.value,
    })
  }
  if (!req) {
    saving.value = false
    return
  }
  req
    .then((node) => {
      dialog.value = null
      reloadKey.value += 1
      loadScopesIndex()
      if (node?.id && (node.kind === 'parameter' || node.kind === 'cli')) {
        selected.value = {
          key: String(node.id),
          id: node.id,
          title: node.name,
          kind: node.kind,
          platform: node.platform,
          payload_kind: node.payload_kind,
          service_type_id: node.service_type_id,
          enabled: node.enabled,
          payload: node.payload,
        }
        loadNodeDetails(selected.value)
      }
    })
    .catch((err) =>
      toast.add({ color: 'error', title: 'Error', description: errMsg(err, 'Request failed.') }),
    )
    .finally(() => {
      saving.value = false
    })
}

function performDelete() {
  const c = confirm.value
  if (!c) return
  saving.value = true
  let req
  if (c.kind === 'scope' || c.kind === 'detach-service') req = deleteScope(c.id)
  if (c.kind === 'detach') req = detachScope(c.id)
  if (c.kind === 'remove-endpoint' && c.node) {
    const node = c.node
    req = getServiceEndpoints(node.service_row_id).then((rows) => {
        const next = (rows ?? []).filter((ep) => !endpointMatchesRef(ep, node))
        return putServiceEndpoints(node.service_row_id, {
          endpoints: next.map((ep) => ({
            role: ep.role,
            device_id: ep.device_id,
            interface_id: ep.interface_id,
            fields: ep.fields || {},
          })),
        })
      })
  }
  if (c.kind === 'variable') req = deleteVariable(c.id).then(loadVariables)
  if (c.kind === 'type') req = deleteServiceType(c.id).then(loadTypes)
  if (c.kind === 'macro') req = deleteMacro(c.id).then(loadMacros)
  if (c.kind === 'assignment')
    req = deleteAssignment(c.id).then(() => loadNodeDetails(selected.value))
  if (!req) {
    saving.value = false
    return
  }
  req
    .then(() => {
      confirm.value = null
      if (
        c.kind === 'scope' ||
        c.kind === 'detach' ||
        c.kind === 'detach-service' ||
        c.kind === 'remove-endpoint'
      ) {
        reloadKey.value += 1
        loadScopesIndex()
      }
    })
    .catch((err) =>
      toast.add({
        color: 'error',
        title: 'Error',
        description: errMsg(err, c.kind === 'detach' ? 'Detach failed.' : 'Delete failed.'),
      }),
    )
    .finally(() => {
      saving.value = false
    })
}

async function loadVariables() {
  try {
    variables.value = (await listVariables()) ?? []
  } catch (err) {
    toast.add({
      color: 'error',
      title: 'Error',
      description: errMsg(err, 'Failed to load variables.'),
    })
  }
}
async function loadTypes() {
  serviceTypes.value = await listServiceTypes().catch(() => [])
}
async function loadMacros() {
  macros.value = await listMacros().catch(() => [])
}

function optionValue(v) {
  if (v && typeof v === 'object' && !Array.isArray(v) && 'value' in v) return v.value
  return v
}

function openVar(row) {
  formError.value = ''
  form.value = row
    ? {
        ...row,
        type: optionValue(row.type) || 'string',
        default_text:
          row.secret || row.type === 'secret'
            ? ''
            : row.default_value != null
              ? typeof row.default_value === 'string'
                ? row.default_value
                : JSON.stringify(row.default_value)
              : '',
        constraints_text: row.constraints != null ? JSON.stringify(row.constraints, null, 2) : '',
      }
    : {
        name: '',
        type: 'string',
        required: false,
        secret: false,
        default_text: '',
        constraints_text: '',
      }
  dialog.value = 'variable'
}

function parseJSON(text, fallback) {
  const s = (text ?? '').trim()
  if (!s) return fallback
  return JSON.parse(s)
}

function parseDefault(text, type) {
  const s = (text ?? '').trim()
  if (!s) return undefined
  try {
    return JSON.parse(s)
  } catch {
    if (type === 'int' || type === 'vlan' || type === 'bool' || type === 'list' || type === 'map') {
      throw new Error('Default must be valid JSON for this type.')
    }
    return s
  }
}

function saveVariable() {
  formError.value = ''
  const name = (form.value.name ?? '').trim()
  const type = optionValue(form.value.type)
  if (!name) {
    formError.value = 'Name is required.'
    return
  }
  if (!type) {
    formError.value = 'Type is required.'
    return
  }
  saving.value = true
  let defaultValue
  let constraints
  try {
    defaultValue = parseDefault(form.value.default_text, type)
    constraints = parseJSON(form.value.constraints_text, undefined)
  } catch (e) {
    saving.value = false
    formError.value = e.message || 'Default/constraints must be valid JSON.'
    toast.add({ color: 'error', title: 'Error', description: formError.value })
    return
  }
  const payload = {
    name,
    type,
    description: form.value.description ?? '',
    required: !!form.value.required,
    secret: !!form.value.secret || type === 'secret',
  }
  if (defaultValue !== undefined) payload.default_value = defaultValue
  if (constraints !== undefined) payload.constraints = constraints
  if (payload.secret) {
    const raw = (form.value.default_text ?? '').trim()
    if (!raw || raw === '***' || raw === '"***"') {
      delete payload.default_value
    }
  }
  const req = form.value.id ? updateVariable(form.value.id, payload) : createVariable(payload)
  req
    .then(() => {
      dialog.value = null
      toast.add({
        color: 'success',
        title: 'Successful',
        description: form.value.id ? 'Variable updated' : 'Variable created',
      })
      return loadVariables()
    })
    .catch((err) => {
      formError.value = errMsg(err, 'Save failed.')
      toast.add({ color: 'error', title: 'Error', description: formError.value })
    })
    .finally(() => {
      saving.value = false
    })
}

function openAssign(row) {
  const def = variables.value.find((v) => v.id === row?.variable_def_id)
  const val = row?.value
  form.value = row?.variable_def_id
    ? {
        id: row.id,
        variable_def_id: row.variable_def_id,
        value_text:
          val == null || typeof val === 'string' ? (val ?? '') : JSON.stringify(val, null, 2),
        value_bool: !!val,
        value_number: typeof val === 'number' ? val : val == null || val === '' ? null : Number(val),
      }
    : { variable_def_id: null, value_text: '', value_bool: false, value_number: null }
  if (def) form.value.variable_def_id = def.id
  dialog.value = 'assign'
}

function assignmentValue(def) {
  const type = def?.type
  if (type === 'bool') return !!form.value.value_bool
  if (type === 'int' || type === 'vlan') {
    const n = form.value.value_number
    return n === null || n === undefined || n === '' ? null : Number(n)
  }
  const raw = optionValue(form.value.value_text)
  if (type === 'list' || type === 'map') {
    const s = (raw ?? '').toString().trim()
    if (!s) return null
    return JSON.parse(s)
  }
  if (raw === null || raw === undefined) return null
  if (typeof raw === 'string') {
    const s = raw.trim()
    return s === '' ? null : s
  }
  return raw
}

function saveAssign() {
  const defId = optionValue(form.value.variable_def_id)
  const scopeId = selected.value?.id
  if (!scopeId || !defId) return
  const def = variables.value.find((v) => v.id === defId)
  saving.value = true
  let value
  try {
    value = assignmentValue(def)
  } catch {
    saving.value = false
    toast.add({
      color: 'error',
      title: 'Error',
      description: 'Value must be valid JSON for this type.',
    })
    return
  }
  const payload = {
    variable_def_id: defId,
    scope_id: scopeId,
  }
  const secret = !!(def && (def.secret || def.type === 'secret'))
  if (!(secret && value === '***')) {
    payload.value = value
  }
  upsertAssignment(payload)
    .then(() => {
      dialog.value = null
      return loadNodeDetails(selected.value)
    })
    .catch((err) =>
      toast.add({ color: 'error', title: 'Error', description: errMsg(err, 'Save failed.') }),
    )
    .finally(() => {
      saving.value = false
    })
}

function loadMatrix() {
  const start = matrixStart.value
  if (!start?.id || !matrixVar.value) {
    matrixRows.value = []
    return
  }
  getMatrix(start.id, matrixVar.value)
    .then((rows) => {
      matrixRows.value = rows
    })
    .catch((err) =>
      toast.add({ color: 'error', title: 'Error', description: errMsg(err, 'Matrix failed.') }),
    )
}

function openType(row) {
  form.value = row
    ? {
        ...row,
        schema_text: JSON.stringify(row.schema ?? [], null, 2),
        roles_text: JSON.stringify(row.endpoint_roles ?? [], null, 2),
      }
    : {
        name: '',
        description: '',
        schema_text: '[]',
        roles_text: '[]',
        sync_source: '',
        netbox_type: '',
      }
  dialog.value = 'type'
}

function saveType() {
  saving.value = true
  let schema
  let roles
  try {
    schema = parseJSON(form.value.schema_text, [])
    roles = parseJSON(form.value.roles_text, [])
  } catch {
    saving.value = false
    toast.add({
      color: 'error',
      title: 'Error',
      description: 'Schema and roles must be valid JSON.',
    })
    return
  }
  const payload = {
    name: form.value.name,
    description: form.value.description ?? '',
    schema,
    endpoint_roles: roles,
    sync_source: optionValue(form.value.sync_source) || '',
    netbox_type: optionValue(form.value.netbox_type) || '',
  }
  const req = form.value.id ? updateServiceType(form.value.id, payload) : createServiceType(payload)
  req
    .then(() => {
      dialog.value = null
      return loadTypes()
    })
    .catch((err) =>
      toast.add({ color: 'error', title: 'Error', description: errMsg(err, 'Save failed.') }),
    )
    .finally(() => {
      saving.value = false
    })
}

function openMacro(row) {
  form.value = row ? { ...row } : { name: '', body: '' }
  dialog.value = 'macro'
}

function saveMacro() {
  saving.value = true
  const payload = { name: form.value.name, body: form.value.body ?? '' }
  const req = form.value.id ? updateMacro(form.value.id, payload) : createMacro(payload)
  req
    .then(() => {
      dialog.value = null
      return loadMacros()
    })
    .catch((err) =>
      toast.add({ color: 'error', title: 'Error', description: errMsg(err, 'Save failed.') }),
    )
    .finally(() => {
      saving.value = false
    })
}

function runPreview() {
  if (!previewDeviceId.value) return
  renderConfig({ device_id: previewDeviceId.value })
    .then((data) => {
      preview.value = data
    })
    .catch((err) =>
      toast.add({ color: 'error', title: 'Error', description: errMsg(err, 'Render failed.') }),
    )
}

async function loadScopesIndex() {
  try {
    const rows = await listScopes()
    const m = {}
    for (const s of rows ?? []) m[s.id] = s
    scopesById.value = m
  } catch {
    scopesById.value = {}
  }
}

function onInspectorSaved() {
  draftEndpoint.value = null
  reloadKey.value += 1
  loadScopesIndex()
  if (selected.value?.id) loadNodeDetails(selected.value)
}

function onDeleteAssignment(row) {
  confirm.value = { kind: 'assignment', id: row.id, label: 'assignment' }
}

function onDocClick() {
  if (menu.value.open) closeMenu()
}

onMounted(() => {
  document.addEventListener('click', onDocClick)
  loadVariables()
  loadTypes()
  loadMacros()
  loadScopesIndex()
  getDevices()
    .then((d) => {
      devices.value = d ?? []
    })
    .catch(() => {})
})
onBeforeUnmount(() => document.removeEventListener('click', onDocClick))
</script>

<template>
  <div class="card flex min-h-0 flex-1 flex-col overflow-hidden">
    <div class="flex flex-wrap gap-2 items-center justify-between mb-3 shrink-0">
      <h4 class="m-0">Config</h4>
      <UButton
        icon="i-lucide-library"
        variant="outline"
        color="neutral"
        label="Catalog"
        @click="catalogOpen = true"
      />
    </div>
    <UTabs
      v-model="tab"
      :items="tabItems"
      class="min-h-0 flex-1"
      :ui="{
        root: 'items-stretch',
        list: 'shrink-0',
        content: 'min-h-0 flex-1 overflow-auto',
      }"
    >
      <template #tree>
        <div
          class="grid min-h-0 flex-1 grid-cols-1 grid-rows-[minmax(16rem,1fr)_auto_auto] gap-4 py-3 lg:grid-cols-2 lg:grid-rows-[minmax(0,1fr)_auto] xl:grid-cols-3 xl:grid-rows-1"
        >
          <div class="flex min-h-0 flex-col overflow-hidden lg:row-span-2 xl:row-span-1">
            <div class="flex flex-wrap gap-2 items-center mb-2 shrink-0">
              <SearchInput
                v-model="filter"
                placeholder="Filter..."
                class="w-56"
                @update:model-value="applyFilter"
              />
              <UButton
                icon="i-lucide-unfold-vertical"
                variant="outline"
                color="neutral"
                size="sm"
                title="Expand one level"
                @click="treeRef?.expandAll()"
              />
              <UButton
                icon="i-lucide-fold-vertical"
                variant="outline"
                color="neutral"
                size="sm"
                title="Collapse all"
                @click="treeRef?.collapseAll()"
              />
            </div>
            <p class="text-muted-color text-sm mb-2 shrink-0">
              The tree is the editor: add parameter objects, CLI objects, and services from the
              context menu. Catalogs (variables, types, macros) live under Catalog.
            </p>
            <ConfigScopeTree
              ref="treeRef"
              :reload-key="reloadKey"
              :can-write="authStore.canWrite"
              class="min-h-0 flex-1"
              @contextmenu="onContextMenu"
              @select="onSelect"
              @move="onMove"
              @rebind="onRebind"
            />
          </div>
          <ConfigNodeInspector
            class="flex min-h-0 flex-col gap-3 overflow-auto"
            :selected="selected"
            :assignments="assignments"
            :resolved="resolved"
            :variables="variables"
            :service-types="serviceTypes"
            :macros="macros"
            :can-write="authStore.canWrite"
            :draft-endpoint="draftEndpoint"
            @assign="openAssign"
            @delete-assignment="onDeleteAssignment"
            @saved="onInspectorSaved"
          />
          <div
            class="flex min-h-0 flex-col gap-3 overflow-auto border-t border-default pt-3 lg:col-start-2 lg:border-t-0 lg:pt-0 xl:col-start-3"
          >
            <h5 class="m-0">Preview</h5>
            <p class="text-muted-color text-sm m-0">
              Desired CLI for a device (baseline CLI objects + terminating services). Does not
              contact the device.
            </p>
            <div class="flex flex-wrap gap-2 items-end">
              <div>
                <label class="block font-bold mb-1">Device</label>
                <USelectMenu
                  v-model="previewDeviceId"
                  :items="deviceOptions"
                  value-key="value"
                  label-key="label"
                  placeholder="Device"
                  class="w-64"
                />
              </div>
              <UButton label="Render" :disabled="!previewDeviceId" @click="runPreview" />
            </div>
            <div v-if="preview" class="flex flex-col gap-3">
              <div
                v-for="(src, i) in preview.sources ?? []"
                :key="i"
                class="border border-default rounded p-3"
              >
                <div class="font-medium mb-2">
                  {{ src.source }} <span class="text-muted-color">{{ src.kind }}</span>
                </div>
                <div v-if="src.error" class="text-red-500 text-sm">{{ src.error }}</div>
                <pre v-else class="text-sm overflow-auto max-h-80">{{
                  (src.commands ?? []).join('\n')
                }}</pre>
              </div>
            </div>
          </div>
        </div>
      </template>

      <template #matrix>
        <div class="flex flex-col gap-3 py-3">
          <p class="text-muted-color text-sm">
            Select a tree node first, then a variable. Rows are interfaces under that folder or
            device (or the nearest such ancestor if you selected a parameter, CLI object, or
            service). Source is the parameter object when the winning assignment lives there.
          </p>
          <div class="flex flex-wrap gap-2 items-end">
            <div>
              <label class="block font-bold mb-1">Variable</label>
              <USelectMenu
                v-model="matrixVar"
                :items="varOptions"
                value-key="value"
                label-key="label"
                placeholder="Variable"
                class="w-56"
              />
            </div>
            <UButton label="Load" :disabled="!matrixStart?.id || !matrixVar" @click="loadMatrix" />
          </div>
          <UTable
            :data="matrixRows"
            :columns="[
              { accessorKey: 'device_name', header: 'Device' },
              { accessorKey: 'interface_name', header: 'Interface' },
              { accessorKey: 'value', header: 'Value' },
              { accessorKey: 'source_name', header: 'Source' },
            ]"
            empty="No rows."
          >
            <template #value-cell="{ row }">
              {{ row.original.error || JSON.stringify(row.original.value) }}
            </template>
          </UTable>
        </div>
      </template>
    </UTabs>
  </div>

  <USlideover
    v-model:open="catalogOpen"
    title="Catalog"
    :ui="{ content: 'max-w-3xl w-full' }"
  >
    <template #body>
      <p class="text-muted-color text-sm m-0 mb-3">
        Definitions used by the tree — not tree nodes themselves.
      </p>
      <UTabs v-model="catalogTab" :items="catalogTabItems">
        <template #variables>
          <div class="flex flex-col gap-3 py-3">
            <p class="text-muted-color text-sm m-0">
              Typed knobs. Assign values on a parameter object in the tree.
            </p>
            <div class="flex justify-end">
              <UButton
                v-if="authStore.canWrite"
                icon="i-lucide-plus"
                label="New"
                @click="openVar(null)"
              />
            </div>
            <UTable
              :data="variables"
              :columns="[
                { accessorKey: 'name', header: 'Name' },
                { accessorKey: 'type', header: 'Type' },
                { accessorKey: 'required', header: 'Required' },
                { accessorKey: 'secret', header: 'Secret' },
                { accessorKey: 'description', header: 'Description' },
                { id: 'actions', header: '' },
              ]"
              empty="No variables."
            >
              <template #required-cell="{ row }">{{ row.original.required ? 'yes' : '' }}</template>
              <template #secret-cell="{ row }">{{ row.original.secret ? 'yes' : '' }}</template>
              <template #actions-cell="{ row }">
                <div class="flex gap-1">
                  <UButton
                    icon="i-lucide-pencil"
                    variant="outline"
                    color="neutral"
                    size="sm"
                    @click="openVar(row.original)"
                  />
                  <UButton
                    v-if="authStore.canWrite"
                    icon="i-lucide-trash-2"
                    variant="outline"
                    color="error"
                    size="sm"
                    @click="
                      confirm = { kind: 'variable', id: row.original.id, label: row.original.name }
                    "
                  />
                </div>
              </template>
            </UTable>
          </div>
        </template>
        <template #types>
          <div class="flex flex-col gap-3 py-3">
            <p class="text-muted-color text-sm m-0">
              Vendor-agnostic classes (ELINE, ELAN, …). CLI for a type lives under
              <code>_catalog/cli</code> in the tree.
            </p>
            <div class="flex justify-end">
              <UButton
                v-if="authStore.canWrite"
                icon="i-lucide-plus"
                label="New"
                @click="openType(null)"
              />
            </div>
            <UTable
              :data="serviceTypes"
              :columns="[
                { accessorKey: 'name', header: 'Name' },
                { accessorKey: 'description', header: 'Description' },
                { accessorKey: 'sync_source', header: 'Sync source' },
                { accessorKey: 'netbox_type', header: 'NetBox type' },
                { accessorKey: 'builtin', header: 'Builtin' },
                { id: 'actions', header: '' },
              ]"
              empty="No service types."
            >
              <template #builtin-cell="{ row }">{{ row.original.builtin ? 'yes' : '' }}</template>
              <template #actions-cell="{ row }">
                <div class="flex gap-1">
                  <UButton
                    icon="i-lucide-pencil"
                    variant="outline"
                    color="neutral"
                    size="sm"
                    @click="openType(row.original)"
                  />
                  <UButton
                    v-if="authStore.canWrite && !row.original.builtin"
                    icon="i-lucide-trash-2"
                    variant="outline"
                    color="error"
                    size="sm"
                    @click="
                      confirm = { kind: 'type', id: row.original.id, label: row.original.name }
                    "
                  />
                </div>
              </template>
            </UTable>
          </div>
        </template>
        <template #macros>
          <div class="flex flex-col gap-3 py-3">
            <p class="text-muted-color text-sm m-0">
              Reusable snippets inserted with
              <code v-pre>{{ include "name" }}</code>
              from a CLI feature.
            </p>
            <div class="flex justify-end">
              <UButton
                v-if="authStore.canWrite"
                icon="i-lucide-plus"
                label="New"
                @click="openMacro(null)"
              />
            </div>
            <UTable
              :data="macros"
              :columns="[
                { accessorKey: 'name', header: 'Name' },
                { id: 'actions', header: '' },
              ]"
              empty="No macros."
            >
              <template #actions-cell="{ row }">
                <div class="flex gap-1">
                  <UButton
                    icon="i-lucide-pencil"
                    variant="outline"
                    color="neutral"
                    size="sm"
                    @click="openMacro(row.original)"
                  />
                  <UButton
                    v-if="authStore.canWrite"
                    icon="i-lucide-trash-2"
                    variant="outline"
                    color="error"
                    size="sm"
                    @click="
                      confirm = { kind: 'macro', id: row.original.id, label: row.original.name }
                    "
                  />
                </div>
              </template>
            </UTable>
          </div>
        </template>
      </UTabs>
    </template>
  </USlideover>

  <div
    v-if="menu.open && menuItems.length"
    class="ipam-context-menu"
    :style="{ left: menu.x + 'px', top: menu.y + 'px' }"
    @click.stop
  >
    <template v-for="(item, i) in menuItems" :key="i">
      <hr v-if="item.id.startsWith('sep')" />
      <button v-else :class="{ danger: item.danger }" type="button" @click="runMenu(item.id)">
        {{ item.label }}
      </button>
    </template>
  </div>

  <UModal
    :open="dialog === 'folder'"
    :title="orgKindTitles[form.kind] || 'Folder'"
    @update:open="(v) => !v && (dialog = null)"
  >
    <template #body>
      <label class="block font-bold mb-2">Name</label>
      <UInput v-model="form.name" autofocus />
    </template>
    <template #footer>
      <UButton label="Cancel" variant="ghost" @click="dialog = null" />
      <UButton label="Save" :loading="saving" @click="saveDialog" />
    </template>
  </UModal>

  <UModal
    :open="dialog === 'parameter'"
    title="Parameter object"
    @update:open="(v) => !v && (dialog = null)"
  >
    <template #body>
      <label class="block font-bold mb-2">Name</label>
      <UInput v-model="form.name" autofocus placeholder="parameters" />
      <p class="text-muted-color text-sm mt-2 m-0">
        Assignments on this node apply to its parent and that parent's descendants.
      </p>
    </template>
    <template #footer>
      <UButton label="Cancel" variant="ghost" @click="dialog = null" />
      <UButton label="Save" :loading="saving" @click="saveDialog" />
    </template>
  </UModal>

  <UModal :open="dialog === 'cli'" title="CLI object" @update:open="(v) => !v && (dialog = null)">
    <template #body>
      <div class="flex flex-col gap-3">
        <div>
          <label class="block font-bold mb-2">Name</label>
          <UInput v-model="form.name" autofocus />
        </div>
        <div>
          <label class="block font-bold mb-2">Platform</label>
          <USelectMenu
            v-model="form.platform"
            :items="platformOptions"
            value-key="value"
            label-key="label"
            class="w-full"
          />
        </div>
      </div>
    </template>
    <template #footer>
      <UButton label="Cancel" variant="ghost" @click="dialog = null" />
      <UButton label="Save" :loading="saving" @click="saveDialog" />
    </template>
  </UModal>

  <UModal
    :open="dialog === 'device'"
    title="Attach device"
    @update:open="(v) => !v && (dialog = null)"
  >
    <template #body>
      <label class="block font-bold mb-2">Device</label>
      <USelectMenu
        v-model="attachDeviceId"
        :items="deviceOptions"
        value-key="value"
        label-key="label"
        placeholder="Select device"
      />
    </template>
    <template #footer>
      <UButton label="Cancel" variant="ghost" @click="dialog = null" />
      <UButton label="Attach" :loading="saving" @click="saveDialog" />
    </template>
  </UModal>

  <UModal
    :open="dialog === 'create-service'"
    title="Create service"
    @update:open="(v) => !v && (dialog = null)"
  >
    <template #body>
      <div class="flex flex-col gap-3">
        <div>
          <label class="block font-bold mb-2">Category</label>
          <USelectMenu
            v-model="form.category"
            :items="categoryOptions"
            value-key="value"
            label-key="label"
            class="w-full"
          />
        </div>
        <div>
          <label class="block font-bold mb-2">Service type</label>
          <USelectMenu
            v-model="form.service_type"
            :items="capacityTypeOptions"
            value-key="value"
            label-key="label"
            class="w-full"
          />
        </div>
        <div>
          <label class="block font-bold mb-2">Company</label>
          <USelectMenu
            v-model="form.company"
            :items="customerOptions"
            value-key="value"
            label-key="label"
            placeholder="Select a customer"
            class="w-full"
          />
        </div>
        <p v-if="form.from_interface" class="text-muted-color text-sm m-0">
          The first endpoint will be pre-filled from this interface and is not saved until you
          complete the set.
        </p>
      </div>
    </template>
    <template #footer>
      <UButton label="Cancel" variant="ghost" @click="dialog = null" />
      <UButton label="Create" :loading="saving" @click="saveDialog" />
    </template>
  </UModal>

  <UModal
    :open="dialog === 'attach-service'"
    title="Attach existing service"
    @update:open="(v) => !v && (dialog = null)"
  >
    <template #body>
      <label class="block font-bold mb-2">Service</label>
      <USelectMenu
        v-model="attachServiceId"
        :items="attachServiceOptions"
        value-key="value"
        label-key="label"
        placeholder="Select a CN/CI service"
      />
    </template>
    <template #footer>
      <UButton label="Cancel" variant="ghost" @click="dialog = null" />
      <UButton label="Attach" :loading="saving" @click="saveDialog" />
    </template>
  </UModal>

  <UModal
    :open="dialog === 'assign'"
    :title="form.id ? 'Edit assignment' : 'Assignment'"
    @update:open="(v) => !v && (dialog = null)"
  >
    <template #body>
      <div class="flex flex-col gap-3">
        <div>
          <label class="block font-bold mb-2">Variable</label>
          <USelectMenu
            v-model="form.variable_def_id"
            :items="variables.map((v) => ({ label: v.name, value: v.id }))"
            value-key="value"
            label-key="label"
            placeholder="Select variable"
            :disabled="!!form.id"
            class="w-full"
          />
        </div>
        <div v-if="assignDef?.type === 'bool'">
          <label class="flex items-center gap-2"
            ><USwitch v-model="form.value_bool" /> Value</label
          >
        </div>
        <div v-else-if="assignDef?.type === 'int' || assignDef?.type === 'vlan'">
          <label class="block font-bold mb-2">Value</label>
          <UInputNumber v-model="form.value_number" class="w-full" />
        </div>
        <div v-else-if="assignDef?.type === 'enum' && assignEnumOptions.length">
          <label class="block font-bold mb-2">Value</label>
          <USelectMenu
            v-model="form.value_text"
            :items="assignEnumOptions"
            value-key="value"
            label-key="label"
            class="w-full"
          />
        </div>
        <div v-else-if="assignDef?.type === 'list' || assignDef?.type === 'map'">
          <label class="block font-bold mb-2">Value (JSON)</label>
          <UTextarea
            v-model="form.value_text"
            :rows="4"
            class="w-full font-mono text-sm"
            :placeholder="assignDef?.type === 'list' ? '[1, 2]' : '{&quot;key&quot;:1}'"
          />
        </div>
        <div v-else>
          <label class="block font-bold mb-2">Value</label>
          <UInput
            v-model="form.value_text"
            :placeholder="
              assignDef?.secret || assignDef?.type === 'secret' ? 'unchanged if ***' : 'value'
            "
          />
        </div>
      </div>
    </template>
    <template #footer>
      <UButton label="Cancel" variant="ghost" @click="dialog = null" />
      <UButton label="Save" :loading="saving" @click="saveAssign" />
    </template>
  </UModal>

  <UModal
    :open="dialog === 'variable'"
    title="Variable"
    :ui="{ content: 'sm:max-w-md' }"
    @update:open="(v) => !v && (dialog = null)"
  >
    <template #body>
      <div class="flex flex-col gap-3">
        <UAlert v-if="formError" color="error" variant="subtle" :title="formError" />
        <div>
          <label class="block font-bold mb-2">Name</label>
          <UInput v-model="form.name" autofocus @keydown.enter.prevent="saveVariable" />
        </div>
        <div>
          <label class="block font-bold mb-2">Type</label>
          <USelect
            v-model="form.type"
            :items="varTypeOptions"
            value-key="value"
            label-key="label"
            class="w-full"
          />
        </div>
        <div>
          <label class="block font-bold mb-2">Description</label>
          <UInput v-model="form.description" />
        </div>
        <div class="flex gap-4">
          <label class="flex items-center gap-2"
            ><USwitch v-model="form.required" /> Required</label
          >
          <label class="flex items-center gap-2"><USwitch v-model="form.secret" /> Secret</label>
        </div>
        <div>
          <label class="block font-bold mb-2">Default</label>
          <UInput
            v-model="form.default_text"
            :placeholder="form.secret || form.type === 'secret' ? 'unchanged' : 'optional'"
          />
        </div>
        <div>
          <label class="block font-bold mb-2">Constraints (JSON, optional)</label>
          <p class="text-muted-color text-sm mb-1">{{ constraintsHint }}</p>
          <UTextarea
            v-model="form.constraints_text"
            :rows="3"
            class="w-full"
            :placeholder="constraintsPlaceholder"
          />
        </div>
      </div>
    </template>
    <template #footer>
      <UButton label="Cancel" variant="ghost" type="button" @click="dialog = null" />
      <UButton label="Save" :loading="saving" type="button" @click="saveVariable" />
    </template>
  </UModal>

  <UModal
    :open="dialog === 'type'"
    title="Service type"
    @update:open="(v) => !v && (dialog = null)"
  >
    <template #body>
      <div class="flex flex-col gap-3">
        <div>
          <label class="block font-bold mb-2">Name</label>
          <UInput v-model="form.name" :disabled="!!form.builtin" />
        </div>
        <div>
          <label class="block font-bold mb-2">Description</label>
          <UInput v-model="form.description" />
        </div>
        <div>
          <label class="block font-bold mb-2">Device-sync source</label>
          <USelectMenu
            v-model="form.sync_source"
            :items="syncSourceOptions"
            value-key="value"
            label-key="label"
            class="w-full"
          />
        </div>
        <div>
          <label class="block font-bold mb-2">NetBox type</label>
          <USelectMenu
            v-model="form.netbox_type"
            :items="netboxTypeOptions"
            value-key="value"
            label-key="label"
            class="w-full"
          />
        </div>
        <div>
          <label class="block font-bold mb-2">Schema (JSON)</label>
          <UTextarea v-model="form.schema_text" :rows="4" class="w-full font-mono text-sm" />
        </div>
        <div>
          <label class="block font-bold mb-2">Endpoint roles (JSON)</label>
          <UTextarea v-model="form.roles_text" :rows="6" class="w-full font-mono text-sm" />
        </div>
      </div>
    </template>
    <template #footer>
      <UButton label="Cancel" variant="ghost" @click="dialog = null" />
      <UButton label="Save" :loading="saving" @click="saveType" />
    </template>
  </UModal>

  <UModal
    :open="dialog === 'macro'"
    title="Macro"
    :ui="{
      content: 'w-[90vw] h-[90vh] sm:max-w-none flex flex-col bg-default',
      body: 'flex flex-1 min-h-0 flex-col overflow-hidden bg-default',
      footer: 'bg-default',
      header: 'bg-default',
    }"
    @update:open="(v) => !v && (dialog = null)"
  >
    <template #body>
      <div class="flex h-full min-h-0 flex-col gap-3">
        <div class="shrink-0">
          <label class="mb-2 block font-bold">Name</label>
          <UInput v-model="form.name" class="w-full" />
        </div>
        <div class="flex min-h-0 flex-1 flex-col">
          <GoTemplateEditor
            v-if="dialog === 'macro'"
            v-model="form.body"
            class="h-full min-h-0"
            :schema="macroSchema"
            :placeholder="macroBodyPlaceholder"
            @apply="saveMacro"
          />
        </div>
      </div>
    </template>
    <template #footer>
      <UButton label="Cancel" variant="ghost" @click="dialog = null" />
      <UButton label="Save" :loading="saving" @click="saveMacro" />
    </template>
  </UModal>

  <UModal
    :open="!!confirm"
    :title="
      confirm?.kind === 'detach' || confirm?.kind === 'detach-service'
        ? 'Confirm detach'
        : 'Confirm delete'
    "
    @update:open="(v) => !v && (confirm = null)"
  >
    <template #body>
      <span v-if="confirm?.kind === 'detach'">
        Detach {{ confirm?.label }} from the tree? The device remains in inventory.
      </span>
      <span v-else-if="confirm?.kind === 'detach-service'">
        Detach {{ confirm?.label }} from the tree? The service remains in inventory.
      </span>
      <span v-else-if="confirm?.kind === 'remove-endpoint'">
        Remove endpoint {{ confirm?.label }} from the service?
      </span>
      <span v-else> Delete {{ confirm?.label }}? </span>
    </template>
    <template #footer>
      <UButton label="Cancel" variant="ghost" @click="confirm = null" />
      <UButton
        :label="
          confirm?.kind === 'detach' || confirm?.kind === 'detach-service' ? 'Detach' : 'Delete'
        "
        color="error"
        :loading="saving"
        @click="performDelete"
      />
    </template>
  </UModal>
</template>
