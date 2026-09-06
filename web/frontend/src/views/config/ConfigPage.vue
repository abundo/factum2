<script setup>
import { useToast } from '@nuxt/ui/composables'
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import {
  createMacro,
  createPlatformPack,
  createScope,
  createServiceType,
  createTemplate,
  createVariable,
  deleteAssignment,
  deleteMacro,
  deletePlatformPack,
  deleteScope,
  deleteServiceType,
  deleteTemplate,
  deleteVariable,
  getMatrix,
  listAssignments,
  listMacros,
  listPlatformPacks,
  listServiceTypes,
  listTemplates,
  listVariables,
  renderConfig,
  resolveInterface,
  updateMacro,
  updatePlatformPack,
  updateServiceType,
  updateTemplate,
  updateVariable,
  upsertAssignment,
} from '@/api/config'
import { getDevices } from '@/api/devices'
import ConfigScopeTree from '@/components/ConfigScopeTree.vue'
import GoTemplateEditor from '@/components/GoTemplateEditor.vue'
import { useAuthStore } from '@/stores/auth'
import {
  cfgmgmtBaselineSchema,
  cfgmgmtMacroSchema,
  cfgmgmtPackSchema,
  withCfgmgmtContext,
} from '@/utils/goTemplateSchemas'

defineOptions({ name: 'ConfigPage' })

const toast = useToast()
const authStore = useAuthStore()
const treeRef = ref(null)
const filter = ref('')
const saving = ref(false)
const tab = ref('tree')
const selected = ref(null)
const reloadKey = ref(0)

const menu = ref({ open: false, x: 0, y: 0, node: null })
const dialog = ref(null)
const form = ref({})
const formError = ref('')
const confirm = ref(null)
const attachDeviceId = ref(null)
const devices = ref([])

const assignments = ref([])
const variables = ref([])
const resolved = ref([])
const matrixVar = ref(null)
const matrixRows = ref([])
const serviceTypes = ref([])
const packs = ref([])
const packTypeId = ref(null)
const macros = ref([])
const templates = ref([])
const previewDeviceId = ref(null)
const preview = ref(null)
const packTmplTab = ref('apply')
const packApplyPlaceholder =
  'Go text/template. {{define "cleanup"}} … {{end}} or use the cleanup tab.'
const packCleanupPlaceholder =
  'Optional teardown. If empty, a {{define "cleanup"}} in apply is used.'
const macroBodyPlaceholder = 'Go text/template. Inserted with {{include "name"}}.'
const baselineBodyPlaceholder = 'Go text/template. Rendered with .Name, .Device, and .Vars.'

const tabItems = [
  {
    label: 'Tree',
    value: 'tree',
    slot: 'tree',
    class: 'flex min-h-0 flex-1 flex-col overflow-auto lg:overflow-hidden',
  },
  { label: 'Matrix', value: 'matrix', slot: 'matrix' },
  { label: 'Variables', value: 'variables', slot: 'variables' },
  { label: 'Service types', value: 'types', slot: 'types' },
  { label: 'Platform packs', value: 'packs', slot: 'packs' },
  { label: 'Macros', value: 'macros', slot: 'macros' },
  { label: 'Templates', value: 'templates', slot: 'templates' },
  { label: 'Preview', value: 'preview', slot: 'preview' },
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
const varOptions = computed(() => variables.value.map((v) => ({ label: v.name, value: v.name })))
const typeOptions = computed(() => serviceTypes.value.map((t) => ({ label: t.name, value: t.id })))

const packSchema = computed(() => {
  const typeId = optionValue(form.value?.service_type_id)
  const serviceType = serviceTypes.value.find((t) => t.id === typeId) ?? null
  return withCfgmgmtContext(cfgmgmtPackSchema, {
    macros: macros.value,
    variables: variables.value,
    serviceType,
  })
})
const macroSchema = computed(() =>
  withCfgmgmtContext(cfgmgmtMacroSchema, {
    macros: macros.value,
    variables: variables.value,
  }),
)
const baselineSchema = computed(() =>
  withCfgmgmtContext(cfgmgmtBaselineSchema, {
    macros: macros.value,
    variables: variables.value,
  }),
)

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
    { id: 'sep' },
    { id: 'add-folder', label: 'Add child folder' },
  ]
  if (node.kind !== 'interface') {
    items.push({ id: 'attach-device', label: 'Attach device' })
  }
  if (node.kind !== 'folder' || node.title !== 'global' || node.parent_id) {
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

function onSelect(node) {
  selected.value = node
  if (node?.id) loadNodeDetails(node)
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
  if (id === 'add-folder') {
    form.value = { parent_id: node?.id, name: '', kind: 'folder' }
    dialog.value = 'folder'
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
  if (id === 'del' && node?.id) {
    confirm.value = { kind: 'scope', id: node.id, label: node.title }
  }
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
      kind: 'folder',
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
  }
  if (!req) {
    saving.value = false
    return
  }
  req
    .then(() => {
      dialog.value = null
      reloadKey.value += 1
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
  if (c.kind === 'scope') req = deleteScope(c.id)
  if (c.kind === 'variable') req = deleteVariable(c.id).then(loadVariables)
  if (c.kind === 'type') req = deleteServiceType(c.id).then(loadTypes)
  if (c.kind === 'pack') req = deletePlatformPack(c.id).then(loadPacks)
  if (c.kind === 'macro') req = deleteMacro(c.id).then(loadMacros)
  if (c.kind === 'template') req = deleteTemplate(c.id).then(loadTemplates)
  if (c.kind === 'assignment')
    req = deleteAssignment(c.id).then(() => loadNodeDetails(selected.value))
  if (!req) {
    saving.value = false
    return
  }
  req
    .then(() => {
      confirm.value = null
      if (c.kind === 'scope') reloadKey.value += 1
    })
    .catch((err) =>
      toast.add({ color: 'error', title: 'Error', description: errMsg(err, 'Delete failed.') }),
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
async function loadPacks() {
  packs.value = await listPlatformPacks(packTypeId.value).catch(() => [])
}
async function loadMacros() {
  macros.value = await listMacros().catch(() => [])
}
async function loadTemplates() {
  templates.value = await listTemplates().catch(() => [])
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
  form.value = row?.variable_def_id
    ? {
        id: row.id,
        variable_def_id: row.variable_def_id,
        value_text: row.value == null ? '' : JSON.stringify(row.value),
      }
    : { variable_def_id: null, value_text: '' }
  dialog.value = 'assign'
}

function saveAssign() {
  if (!selected.value?.id || !form.value.variable_def_id) return
  saving.value = true
  let value
  try {
    const raw = (form.value.value_text ?? '').trim()
    value = raw === '' ? null : JSON.parse(raw)
  } catch {
    value = form.value.value_text
  }
  const payload = {
    variable_def_id: form.value.variable_def_id,
    scope_id: selected.value.id,
  }
  if (value !== '***') {
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
  if (!selected.value?.id || !matrixVar.value) {
    matrixRows.value = []
    return
  }
  getMatrix(selected.value.id, matrixVar.value)
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

function openPack(row) {
  form.value = row
    ? { ...row }
    : {
        service_type_id: packTypeId.value,
        platform: 'eos',
        payload_kind: 'cli',
        apply_template: '',
        cleanup_template: '',
      }
  packTmplTab.value = 'apply'
  dialog.value = 'pack'
}

function savePack() {
  saving.value = true
  const payload = {
    service_type_id: form.value.service_type_id,
    platform: form.value.platform,
    payload_kind: form.value.payload_kind || 'cli',
    apply_template: form.value.apply_template ?? '',
    cleanup_template: form.value.cleanup_template ?? '',
  }
  const req = form.value.id
    ? updatePlatformPack(form.value.id, payload)
    : createPlatformPack(payload)
  req
    .then(() => {
      dialog.value = null
      return loadPacks()
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

function openTemplate(row) {
  form.value = row
    ? { ...row }
    : {
        name: '',
        platform: '',
        payload_kind: 'cli',
        body: '',
        enabled: true,
        scope_id: selected.value?.id,
      }
  dialog.value = 'template'
}

function saveTemplate() {
  saving.value = true
  const payload = {
    name: form.value.name,
    platform: form.value.platform ?? '',
    payload_kind: form.value.payload_kind || 'cli',
    body: form.value.body ?? '',
    enabled: form.value.enabled !== false,
    scope_id: form.value.scope_id || null,
  }
  const req = form.value.id ? updateTemplate(form.value.id, payload) : createTemplate(payload)
  req
    .then(() => {
      dialog.value = null
      return loadTemplates()
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

function assignmentName(id) {
  return variables.value.find((v) => v.id === id)?.name ?? `#${id}`
}

function onDocClick() {
  if (menu.value.open) closeMenu()
}

onMounted(() => {
  document.addEventListener('click', onDocClick)
  loadVariables()
  loadTypes()
  loadPacks()
  loadMacros()
  loadTemplates()
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
          class="grid min-h-0 flex-1 grid-cols-1 grid-rows-[minmax(16rem,1fr)_auto] gap-4 py-3 lg:grid-cols-2 lg:grid-rows-1"
        >
          <div class="flex min-h-0 flex-col overflow-hidden">
            <div class="flex flex-wrap gap-2 items-center mb-2 shrink-0">
              <UInput
                v-model="filter"
                icon="i-lucide-search"
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
              Right-click to add a folder or attach a device. Select a node to edit assignments.
            </p>
            <ConfigScopeTree
              ref="treeRef"
              :reload-key="reloadKey"
              class="min-h-0 flex-1"
              @contextmenu="onContextMenu"
              @select="onSelect"
            />
          </div>
          <div class="flex min-h-0 flex-col gap-3 overflow-auto">
            <div v-if="!selected" class="text-muted-color text-sm">Select a scope node.</div>
            <template v-else>
              <h5 class="m-0">{{ selected.title }}</h5>
              <div class="text-sm text-muted-color">
                Kind: {{ selected.kind }}
                <span v-if="selected.device_id"> · device #{{ selected.device_id }}</span>
                <span v-if="selected.interface_id"> · interface #{{ selected.interface_id }}</span>
              </div>
              <div class="flex items-center justify-between">
                <h6 class="m-0">Assignments</h6>
                <UButton
                  v-if="authStore.canWrite"
                  size="sm"
                  icon="i-lucide-plus"
                  label="Assign"
                  @click="openAssign()"
                />
              </div>
              <UTable
                :data="assignments"
                :columns="[
                  { accessorKey: 'variable_def_id', header: 'Variable' },
                  { accessorKey: 'value', header: 'Value' },
                  { id: 'actions', header: '' },
                ]"
                empty="No assignments on this node."
              >
                <template #variable_def_id-cell="{ row }">
                  {{ assignmentName(row.original.variable_def_id) }}
                </template>
                <template #value-cell="{ row }">
                  {{ JSON.stringify(row.original.value) }}
                </template>
                <template #actions-cell="{ row }">
                  <div class="flex gap-1">
                    <UButton
                      icon="i-lucide-pencil"
                      variant="outline"
                      color="neutral"
                      size="sm"
                      @click="openAssign(row.original)"
                    />
                    <UButton
                      v-if="authStore.canWrite"
                      icon="i-lucide-trash-2"
                      variant="outline"
                      color="error"
                      size="sm"
                      @click="
                        confirm = { kind: 'assignment', id: row.original.id, label: 'assignment' }
                      "
                    />
                  </div>
                </template>
              </UTable>
              <template v-if="selected.kind === 'interface'">
                <h6 class="m-0">Effective values</h6>
                <UTable
                  :data="resolved"
                  :columns="[
                    { accessorKey: 'name', header: 'Variable' },
                    { accessorKey: 'value', header: 'Value' },
                    { accessorKey: 'source_name', header: 'Source' },
                  ]"
                  empty="No variables defined."
                >
                  <template #value-cell="{ row }">
                    {{ row.original.error || JSON.stringify(row.original.value) }}
                  </template>
                  <template #source_name-cell="{ row }">
                    {{ row.original.from_default ? 'default' : row.original.source_name || '—' }}
                  </template>
                </UTable>
              </template>
            </template>
          </div>
        </div>
      </template>

      <template #matrix>
        <div class="flex flex-col gap-3 py-3">
          <p class="text-muted-color text-sm">
            Select a tree node first, then a variable. Rows are interfaces under that node.
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
            <UButton label="Load" :disabled="!selected?.id || !matrixVar" @click="loadMatrix" />
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

      <template #variables>
        <div class="flex flex-col gap-3 py-3">
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
                  @click="confirm = { kind: 'type', id: row.original.id, label: row.original.name }"
                />
              </div>
            </template>
          </UTable>
        </div>
      </template>

      <template #packs>
        <div class="flex flex-col gap-3 py-3">
          <div class="flex flex-wrap gap-2 items-end justify-between">
            <div>
              <label class="block font-bold mb-1">Service type</label>
              <USelectMenu
                v-model="packTypeId"
                :items="[{ label: 'All', value: null }, ...typeOptions]"
                value-key="value"
                label-key="label"
                class="w-56"
                @update:model-value="loadPacks"
              />
            </div>
            <UButton
              v-if="authStore.canWrite"
              icon="i-lucide-plus"
              label="New"
              @click="openPack(null)"
            />
          </div>
          <UTable
            :data="packs"
            :columns="[
              { accessorKey: 'platform', header: 'Platform' },
              { accessorKey: 'payload_kind', header: 'Payload' },
              { accessorKey: 'service_type_id', header: 'Type ID' },
              { id: 'actions', header: '' },
            ]"
            empty="No packs."
          >
            <template #actions-cell="{ row }">
              <div class="flex gap-1">
                <UButton
                  icon="i-lucide-pencil"
                  variant="outline"
                  color="neutral"
                  size="sm"
                  @click="openPack(row.original)"
                />
                <UButton
                  v-if="authStore.canWrite"
                  icon="i-lucide-trash-2"
                  variant="outline"
                  color="error"
                  size="sm"
                  @click="
                    confirm = { kind: 'pack', id: row.original.id, label: row.original.platform }
                  "
                />
              </div>
            </template>
          </UTable>
        </div>
      </template>

      <template #macros>
        <div class="flex flex-col gap-3 py-3">
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

      <template #templates>
        <div class="flex flex-col gap-3 py-3">
          <div class="flex justify-end">
            <UButton
              v-if="authStore.canWrite"
              icon="i-lucide-plus"
              label="New"
              @click="openTemplate(null)"
            />
          </div>
          <UTable
            :data="templates"
            :columns="[
              { accessorKey: 'name', header: 'Name' },
              { accessorKey: 'platform', header: 'Platform' },
              { accessorKey: 'enabled', header: 'Enabled' },
              { id: 'actions', header: '' },
            ]"
            empty="No baseline templates."
          >
            <template #enabled-cell="{ row }">{{ row.original.enabled ? 'yes' : '' }}</template>
            <template #actions-cell="{ row }">
              <div class="flex gap-1">
                <UButton
                  icon="i-lucide-pencil"
                  variant="outline"
                  color="neutral"
                  size="sm"
                  @click="openTemplate(row.original)"
                />
                <UButton
                  v-if="authStore.canWrite"
                  icon="i-lucide-trash-2"
                  variant="outline"
                  color="error"
                  size="sm"
                  @click="
                    confirm = { kind: 'template', id: row.original.id, label: row.original.name }
                  "
                />
              </div>
            </template>
          </UTable>
        </div>
      </template>

      <template #preview>
        <div class="flex flex-col gap-3 py-3">
          <p class="text-muted-color text-sm">
            Render desired configuration for a device (baseline templates + terminating services).
            Does not contact the device.
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
      </template>
    </UTabs>
  </div>

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

  <UModal :open="dialog === 'folder'" title="Folder" @update:open="(v) => !v && (dialog = null)">
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
        <div>
          <label class="block font-bold mb-2">Value (JSON)</label>
          <UInput v-model="form.value_text" placeholder='"text", 100, true' />
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
    :open="dialog === 'pack'"
    title="Platform pack"
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
        <div class="grid shrink-0 grid-cols-1 gap-3 sm:grid-cols-3">
          <div>
            <label class="mb-2 block font-bold">Service type</label>
            <USelectMenu
              v-model="form.service_type_id"
              :items="typeOptions"
              value-key="value"
              label-key="label"
              class="w-full"
            />
          </div>
          <div>
            <label class="mb-2 block font-bold">Platform</label>
            <USelectMenu
              v-model="form.platform"
              :items="platformOptions"
              value-key="value"
              label-key="label"
              class="w-full"
            />
          </div>
          <div>
            <label class="mb-2 block font-bold">Payload kind</label>
            <UInput v-model="form.payload_kind" placeholder="cli" class="w-full" />
          </div>
        </div>
        <div class="flex shrink-0 gap-1">
          <UButton
            :key="`pack-apply-${packTmplTab}`"
            size="sm"
            :variant="packTmplTab === 'apply' ? 'solid' : 'outline'"
            :color="packTmplTab === 'apply' ? 'primary' : 'neutral'"
            label="Apply template"
            @click="packTmplTab = 'apply'"
          />
          <UButton
            :key="`pack-cleanup-${packTmplTab}`"
            size="sm"
            :variant="packTmplTab === 'cleanup' ? 'solid' : 'outline'"
            :color="packTmplTab === 'cleanup' ? 'primary' : 'neutral'"
            label="Cleanup template"
            @click="packTmplTab = 'cleanup'"
          />
        </div>
        <div class="flex min-h-0 flex-1 flex-col">
          <GoTemplateEditor
            v-if="dialog === 'pack' && packTmplTab === 'apply'"
            v-model="form.apply_template"
            class="h-full min-h-0"
            :schema="packSchema"
            :placeholder="packApplyPlaceholder"
            @apply="savePack"
          />
          <GoTemplateEditor
            v-if="dialog === 'pack' && packTmplTab === 'cleanup'"
            v-model="form.cleanup_template"
            class="h-full min-h-0"
            :schema="packSchema"
            :placeholder="packCleanupPlaceholder"
            @apply="savePack"
          />
        </div>
      </div>
    </template>
    <template #footer>
      <UButton label="Cancel" variant="ghost" @click="dialog = null" />
      <UButton label="Save" :loading="saving" @click="savePack" />
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
    :open="dialog === 'template'"
    title="Baseline template"
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
        <div class="grid shrink-0 grid-cols-1 gap-3 sm:grid-cols-2">
          <div>
            <label class="mb-2 block font-bold">Name</label>
            <UInput v-model="form.name" class="w-full" />
          </div>
          <div>
            <label class="mb-2 block font-bold">Platform (empty = all)</label>
            <UInput v-model="form.platform" class="w-full" />
          </div>
        </div>
        <div class="shrink-0">
          <label class="flex items-center gap-2"><USwitch v-model="form.enabled" /> Enabled</label>
        </div>
        <div class="flex min-h-0 flex-1 flex-col">
          <GoTemplateEditor
            v-if="dialog === 'template'"
            v-model="form.body"
            class="h-full min-h-0"
            :schema="baselineSchema"
            :placeholder="baselineBodyPlaceholder"
            @apply="saveTemplate"
          />
        </div>
      </div>
    </template>
    <template #footer>
      <UButton label="Cancel" variant="ghost" @click="dialog = null" />
      <UButton label="Save" :loading="saving" @click="saveTemplate" />
    </template>
  </UModal>

  <UModal :open="!!confirm" title="Confirm delete" @update:open="(v) => !v && (confirm = null)">
    <template #body> Delete {{ confirm?.label }}? </template>
    <template #footer>
      <UButton label="Cancel" variant="ghost" @click="confirm = null" />
      <UButton label="Delete" color="error" :loading="saving" @click="performDelete" />
    </template>
  </UModal>
</template>
