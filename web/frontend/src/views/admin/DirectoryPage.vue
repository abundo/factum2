<script setup>
import { useToast } from '@nuxt/ui/composables'
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { Wunderbaum } from 'wunderbaum'
import {
  browseLdap,
  createLdapGroup,
  createLdapPerson,
  createLdapServer,
  deleteLdapGroup,
  deleteLdapPerson,
  deleteLdapServer,
  getLdapEntry,
  getLdapServers,
  reorderLdapServers,
  testLdapConnection,
  updateLdapGroup,
  updateLdapPerson,
  updateLdapServer,
} from '@/api/ldap'
import {
  createLdapOU,
  deleteLdapOU,
  getLdapInternal,
  placeLdap,
  setRoleMembers,
  updateLdapInternal,
  updateLdapOU,
} from '@/api/ldapInternal'
import { createRole, getRoles, updateRole } from '@/api/roles'
import { createUser, deleteUser, getUsers, updateUser } from '@/api/users'
import PageFormBar from '@/components/PageFormBar.vue'
import AuthorizationSettings from './AuthorizationSettings.vue'
import LdapTreeBrowser from '@/components/LdapTreeBrowser.vue'
import PasswordInput from '@/components/PasswordInput.vue'
import SearchInput from '@/components/SearchInput.vue'
import { useConfirm } from '@/composables/useConfirm'
import { confirmDiscard, usePageForm } from '@/composables/useFormGuard'
import {
  internalDropRegions,
  ldapKindLabel,
  serverDropRegions,
  serverOrderAfterDrop,
  sortDirectoryNodes,
  toLdapNode,
} from '@/utils/ldapTree'
import { searchWords } from '@/utils/search'
import 'wunderbaum/dist/wunderbaum.css'
import '@/assets/wunderbaum-theme.css'

defineOptions({ name: 'DirectoryPage' })

const route = useRoute()
const router = useRouter()
const toast = useToast()

// Authorization is a tab on this page. The query keeps a direct link working.
const directoryTabs = [
  { label: 'Directory', value: 'directory', slot: 'directory', icon: 'i-lucide-folder-tree' },
  { label: 'Authorization', value: 'authorization', slot: 'authorization', icon: 'i-lucide-lock' },
]
const tab = computed({
  get: () => (route.query.tab === 'authorization' ? 'authorization' : 'directory'),
  set: (value) =>
    router.replace({
      query: { ...route.query, tab: value === 'directory' ? undefined : value },
    }),
})
const { confirmDelete, ask } = useConfirm()

const el = ref(null)
const users = ref([])
const roles = ref([])
const servers = ref([])
const internal = ref({
  enabled: false,
  listen: '',
  tls_listen: '',
  tls_certificate_id: null,
  certificates: [],
  base_dn: '',
  bind_dn: '',
  bind_password_set: false,
  radius_use_internal: false,
  user_filter: '(uid=%s)',
  ous: [],
  placements: [],
  nests: [],
})
const internalForm = ref({})
const ouForm = ref({})
const loading = ref(true)
const error = ref(null)
const forbidden = ref(false)
const selected = ref(null)
const search = ref('')
const panel = ref('')
const directoryFormId = computed(() =>
  panel.value && panel.value !== 'node' ? `directory-${panel.value}-form` : '',
)
const directoryCanSave = computed(() => {
  if (!directoryFormId.value) return false
  if (panel.value === 'person') return !!person.value?.editable
  if (panel.value === 'group') return !!group.value?.editable
  return true
})
const directorySaveLoading = computed(() => {
  if (panel.value === 'user') return userSaving.value
  if (panel.value === 'role') return roleSaving.value
  if (panel.value === 'person') return personSaving.value
  if (panel.value === 'group') return groupSaving.value
  if (panel.value === 'server') return serverSaving.value
  return false
})
const submitted = ref(false)
const creating = ref(false)
let tree
let onContextMenu
let onExpanderDown
let onExpanderUp
let expanderPress = null
let fillSeq = 0
let allowActivate = false

const menu = ref({ open: false, x: 0, y: 0, node: null })

const user = ref({})
const userSaving = ref(false)
const userDeleting = ref(false)

const role = ref({})
const roleSaving = ref(false)

const person = ref({})
const personSaving = ref(false)
const personLoading = ref(false)
const personDeleting = ref(false)

const group = ref({})
const groupSaving = ref(false)
const groupLoading = ref(false)
const groupDeleting = ref(false)
const memberBrowser = ref(false)

const activeForm = computed(() => {
  if (panel.value === 'user') return user.value
  if (panel.value === 'role') return role.value
  if (panel.value === 'person') return person.value
  if (panel.value === 'group') return group.value
  if (panel.value === 'internal') return internalForm.value
  if (panel.value === 'ou') return ouForm.value
  if (panel.value === 'server') return serverForm.value
  return null
})
const { mark, dirty: formDirty } = usePageForm(activeForm)

const serverForm = ref({})
const serverSaving = ref(false)
const serverDeleting = ref(false)
const serverTesting = ref(false)

const tlsModeOptions = [
  { label: 'None', value: 'none' },
  { label: 'StartTLS', value: 'starttls' },
  { label: 'LDAPS', value: 'ldaps' },
]
const serverTypeOptions = [
  { label: 'Active Directory', value: 'ad' },
  { label: 'OpenLDAP', value: 'generic' },
]
const bindModeOptions = [
  { label: 'Anonymous bind', value: 'anonymous' },
  { label: 'Service account bind', value: 'service' },
]

function serverOf(id) {
  return servers.value.find((s) => s.id === id)
}

function nodeWritable(node) {
  return !!serverOf(node?.server_id)?.writable
}

const intro = computed(() => {
  if (!servers.value.length) {
    return 'People and roles in this directory are served to RADIUS, NetBox, LibreNMS, and Icinga. Right-click to add an OpenLDAP or Active Directory server.'
  }
  return 'Login tries each enabled server in order. If none accepts the password, the client is denied. OpenLDAP entries can be changed when ldap_writeback is set. Active Directory is browse-only.'
})

const menuItems = computed(() => {
  if (!menu.value.open) return []
  const node = menu.value.node
  const kind = node?.kind || ''
  const items = []
  if (!node) {
    items.push({ id: 'new-user', label: 'New user' })
    items.push({ id: 'new-role', label: 'New role' })
  }
  const ldapHere =
    kind === 'server' || kind === 'directory' || kind === 'container' || kind === 'other'
  if (nodeWritable(node) && ldapHere) {
    items.push({ id: 'new-person', label: 'New directory user' })
    items.push({ id: 'new-group', label: 'New group' })
  }
  if (!node || kind === 'server' || kind === 'internal') {
    items.push({ id: 'new-openldap', label: 'New OpenLDAP server' })
    items.push({ id: 'new-ad', label: 'New Active Directory' })
  }
  if (kind === 'internal' || kind === 'ou') {
    items.push({ id: 'new-folder', label: 'New folder' })
    items.push({ id: 'new-user', label: 'New user' })
    items.push({ id: 'new-role', label: 'New role' })
  }
  if (
    kind === 'user' ||
    kind === 'role' ||
    kind === 'person' ||
    kind === 'group' ||
    kind === 'ou' ||
    kind === 'server'
  ) {
    items.push({ id: 'edit', label: 'Edit' })
  }
  if (kind === 'server') items.push({ id: 'delete', label: 'Delete', danger: true })
  if (kind === 'ou') items.push({ id: 'delete', label: 'Delete', danger: true })
  if (kind === 'user' && !isAdmin(node)) items.push({ id: 'delete', label: 'Delete', danger: true })
  if ((kind === 'person' || kind === 'group') && nodeWritable(node)) {
    items.push({ id: 'delete', label: 'Delete', danger: true })
  }
  return items
})

const selectedKind = computed(() => selected.value?.kind || '')

function errMsg(err, fallback) {
  return err.response?.data?.error ?? err.message ?? fallback
}

function toastError(err, fallback) {
  toast.add({ color: 'error', title: 'Error', description: errMsg(err, fallback), duration: 4000 })
}

function toastOk(description) {
  toast.add({ color: 'success', title: 'Successful', description, duration: 3000 })
}

function payload(node) {
  if (!node) return null
  return { key: node.key, title: node.title, type: node.type, ...(node.data ?? {}) }
}

function ouOf(kind, id) {
  const row = (internal.value.placements || []).find((p) => p.kind === kind && p.id === id)
  return row?.ou_id || 0
}

function memberRoleIDs(roleId) {
  return (internal.value.nests || [])
    .filter((n) => n.role_id === roleId)
    .map((n) => n.member_role_id)
}

function folderItems(excludeId) {
  const hidden = new Set()
  if (excludeId) {
    const stack = [excludeId]
    while (stack.length) {
      const id = stack.pop()
      if (hidden.has(id)) continue
      hidden.add(id)
      for (const ou of internal.value.ous || []) {
        if (ou.parent_id === id) stack.push(ou.id)
      }
    }
  }
  const items = [{ label: 'Default', value: 0 }]
  const walk = (parent, depth) => {
    const rows = (internal.value.ous || [])
      .filter((ou) => (ou.parent_id || 0) === parent && !hidden.has(ou.id))
      .slice()
      .sort((a, b) => a.name.localeCompare(b.name, undefined, { sensitivity: 'base' }))
    for (const ou of rows) {
      items.push({ label: `${depth ? '· '.repeat(depth) : ''}${ou.name}`, value: ou.id })
      walk(ou.id, depth + 1)
    }
  }
  walk(0, 0)
  return items
}

const userFolderItems = computed(() => folderItems(0))
const roleFolderItems = computed(() => folderItems(0))
const ouParentItems = computed(() => folderItems(ouForm.value.id || 0))
const ldapsCertItems = computed(() => {
  const items = [{ label: 'Worker hub certificate', value: 0 }]
  for (const cert of internal.value.certificates || []) {
    const domains = (cert.domains || []).filter(Boolean).join(', ')
    items.push({
      label: domains ? `${cert.name} (${domains})` : cert.name,
      value: cert.id,
    })
  }
  return items
})
const nestRoleItems = computed(() => roles.value.filter((r) => r.id !== role.value.id))

function dirUserNode(u) {
  return {
    key: `dir-user-${u.id}`,
    title: u.username,
    type: 'user',
    kind: 'user',
    user_id: u.id,
    username: u.username,
    name: u.name || '',
    email: u.email || '',
    mobile: u.mobile || '',
    role_ids: u.role_ids ?? [],
    ou_id: ouOf('user', u.id),
  }
}

function dirRoleNode(r) {
  return {
    key: `dir-role-${r.id}`,
    title: r.name,
    type: 'role',
    kind: 'role',
    role_id: r.id,
    name: r.name,
    description: r.description || '',
    ou_id: ouOf('role', r.id),
  }
}

function ouNodes(parentId) {
  return (internal.value.ous || [])
    .filter((ou) => (ou.parent_id || 0) === parentId)
    .slice()
    .sort((a, b) => a.name.localeCompare(b.name, undefined, { sensitivity: 'base' }))
    .map((ou) => ({
      key: `ou-${ou.id}`,
      title: ou.name,
      type: 'ou',
      kind: 'ou',
      ou_id: ou.id,
      name: ou.name,
      parent_id: ou.parent_id || 0,
      dn: ou.dn || '',
      expanded: true,
      children: folderChildren(ou.id),
    }))
}

function folderChildren(parentId) {
  return sortDirectoryNodes([
    ...ouNodes(parentId),
    ...users.value.filter((u) => (ouOf('user', u.id) || 0) === parentId).map(dirUserNode),
    ...roles.value.filter((r) => (ouOf('role', r.id) || 0) === parentId).map(dirRoleNode),
  ])
}

function internalBranch() {
  const base = internal.value.base_dn || 'Internal'
  return {
    key: 'internal',
    title: base,
    type: 'internal',
    kind: 'internal',
    dn: internal.value.base_dn || '',
    expanded: true,
    children: folderChildren(0),
  }
}

function serverTitle(s) {
  return s?.name || s?.host || 'LDAP server'
}

function serverBranch(s) {
  return {
    key: `ldap-server-${s.id}`,
    title: serverTitle(s),
    type: 'server',
    kind: 'server',
    server_id: s.id,
    dn: s.base_dn || '',
    lazy: !!(s.host && s.base_dn),
  }
}

function toServerNode(entry, serverId) {
  const node = toLdapNode(entry)
  node.server_id = serverId
  node.key = `s${serverId}:${entry.dn}`
  return node
}

function ldapNodeKey(serverId, dn) {
  const server = serverOf(serverId)
  const base = (server?.base_dn || '').toLowerCase()
  if (!dn || (base && dn.toLowerCase() === base)) return `ldap-server-${serverId}`
  return `s${serverId}:${dn}`
}

function renderCell(e) {
  for (const col of Object.values(e.renderColInfosById ?? {})) {
    if (col.id === 'kind') {
      col.elem.textContent = ldapKindLabel(e.node.data?.kind || e.node.type)
    }
  }
}

function eventElement(ev) {
  const target = ev.target
  return target?.nodeType === 3 ? target.parentElement : target
}

function expanderFromEvent(ev) {
  const expander = eventElement(ev)?.closest?.('i.wb-expander')
  if (!expander || expander.classList.contains('wb-indent')) return null
  return expander
}

function isBranch(node) {
  if (!node) return false
  const kind = nodeKind(node)
  return !['user', 'role', 'person', 'group'].includes(kind)
}

function nodeKind(node) {
  return node?.data?.kind || node?.type || ''
}

// parentFolderId is the internal folder that holds node. 0 is the local
// root. null means the node is not a user, role, or folder.
function parentFolderId(node) {
  const parent = node?.parent
  if (!parent || parent === tree?.root) return null
  const kind = nodeKind(parent)
  if (kind === 'internal') return 0
  if (kind === 'ou') return parent.data?.ou_id || 0
  return null
}

function ouDescendsFrom(destId, ancestorId) {
  if (!destId || !ancestorId) return false
  let id = destId
  const seen = new Set()
  while (id) {
    if (id === ancestorId) return true
    if (seen.has(id)) return false
    seen.add(id)
    const ou = (internal.value.ous || []).find((row) => row.id === id)
    id = ou?.parent_id || 0
  }
  return false
}

function internalRegions(source, target) {
  const sourceParent = parentFolderId(source)
  if (sourceParent == null) return false
  const targetKind = nodeKind(target)
  const overId =
    targetKind === 'ou' ? target.data?.ou_id || 0 : targetKind === 'internal' ? 0 : null
  const dest = overId != null ? overId : parentFolderId(target)
  const intoSelf =
    nodeKind(source) === 'ou' && dest != null && ouDescendsFrom(dest, source.data?.ou_id || 0)
  return internalDropRegions({
    sourceParent,
    targetKind,
    overId,
    aroundParent: parentFolderId(target),
    intoSelf,
  })
}

function dropFolderId(target, mode) {
  if (mode === 'appendChild') {
    const kind = nodeKind(target)
    if (kind === 'internal') return 0
    if (kind === 'ou') return target.data?.ou_id || 0
    return null
  }
  return parentFolderId(target)
}

function setPlacement(kind, id, ouId) {
  const list = (internal.value.placements || []).filter((p) => !(p.kind === kind && p.id === id))
  if (ouId) list.push({ kind, id, ou_id: ouId })
  internal.value = { ...internal.value, placements: list }
}

function setOuParent(id, parentId) {
  const ous = (internal.value.ous || []).map((ou) =>
    ou.id === id ? { ...ou, parent_id: parentId || null } : ou,
  )
  internal.value = { ...internal.value, ous }
}

// patchOpenFolder keeps an open form on the saved folder without marking a
// clean form dirty. Other unsaved fields stay unsaved.
async function patchOpenFolder(kind, id, dest) {
  const folder = dest || 0
  let touched = false
  if (kind === 'user' && panel.value === 'user' && user.value.id === id) {
    touched = true
    user.value.ou_id = folder
  } else if (kind === 'role' && panel.value === 'role' && role.value.id === id) {
    touched = true
    role.value.ou_id = folder
  } else if (kind === 'ou' && panel.value === 'ou' && ouForm.value.id === id) {
    touched = true
    ouForm.value.parent_id = folder
  }
  if (!touched) return
  if (selected.value?.key === (kind === 'ou' ? `ou-${id}` : `dir-${kind}-${id}`)) {
    selected.value = {
      ...selected.value,
      ou_id: kind === 'ou' ? selected.value.ou_id : folder,
      parent_id: kind === 'ou' ? folder : selected.value.parent_id,
    }
  }
  await nextTick()
  mark()
}

function rootServerIds() {
  return (tree?.root?.children ?? [])
    .filter((node) => nodeKind(node) === 'server')
    .map((node) => node.data?.server_id)
    .filter(Boolean)
}

let treeDrop = null

function onTreeDrop(e) {
  if (treeDrop) return
  const source = e.sourceNode
  const target = e.node
  const mode = e.suggestedDropMode
  if (!source || !target || !mode) return
  const kind = nodeKind(source)
  treeDrop = (
    kind === 'server' ? dropServer(source, target, mode) : dropInternal(source, target, mode)
  ).finally(() => {
    treeDrop = null
  })
}

async function dropInternal(source, target, mode) {
  const kind = nodeKind(source)
  if (kind !== 'user' && kind !== 'role' && kind !== 'ou') return
  const dest = dropFolderId(target, mode)
  const current = parentFolderId(source)
  if (dest == null || dest === current) return
  if (kind === 'ou' && ouDescendsFrom(dest, source.data?.ou_id || 0)) return
  const key = source.key
  const activeKey = tree.getActiveNode?.()?.key || null
  try {
    if (kind === 'user' || kind === 'role') {
      const id = kind === 'user' ? source.data.user_id : source.data.role_id
      await placeLdap({ kind, id, ou_id: dest || null })
      setPlacement(kind, id, dest)
      if (!formDirty()) await patchOpenFolder(kind, id, dest)
      else if (
        (kind === 'user' && panel.value === 'user' && user.value.id === id) ||
        (kind === 'role' && panel.value === 'role' && role.value.id === id)
      ) {
        if (kind === 'user') user.value.ou_id = dest || 0
        else role.value.ou_id = dest || 0
      }
    } else {
      const id = source.data.ou_id
      const ou = (internal.value.ous || []).find((row) => row.id === id)
      if (!ou) return
      await updateLdapOU(id, { name: ou.name, parent_id: dest || null })
      setOuParent(id, dest)
      if (!formDirty()) await patchOpenFolder('ou', id, dest)
      else if (panel.value === 'ou' && ouForm.value.id === id) ouForm.value.parent_id = dest || 0
    }
    syncInternal()
    if (activeKey) await tree.findKey(activeKey)?.setActive(true, { noEvents: true })
    else await tree.findKey(key)?.makeVisible?.()
  } catch (err) {
    toastError(err, 'Could not move that entry.')
    await reloadLocal()
  }
}

async function dropServer(source, target, mode) {
  const ids = serverOrderAfterDrop(
    rootServerIds(),
    source.data?.server_id,
    nodeKind(target),
    target.data?.server_id,
    mode,
  )
  if (!ids) return
  const previous = rootServerIds()
  if (ids.join(',') === previous.join(',')) return
  const region = mode === 'appendChild' ? 'after' : mode
  const activeKey = tree.getActiveNode?.()?.key || null
  try {
    source.moveTo(target, region)
    await reorderLdapServers(ids)
    const byId = new Map(servers.value.map((row) => [row.id, { ...row }]))
    servers.value = ids
      .map((id, index) => {
        const row = byId.get(id)
        if (row) row.position = index + 1
        return row
      })
      .filter(Boolean)
    const moved = source.data?.server_id
    const position = ids.indexOf(moved) + 1
    if (panel.value === 'server' && serverForm.value.id === moved && position > 0) {
      const dirty = formDirty()
      serverForm.value.position = position
      if (!dirty) {
        await nextTick()
        mark()
      }
    }
    if (activeKey) await tree.findKey(activeKey)?.setActive(true, { noEvents: true })
  } catch (err) {
    toastError(err, 'Could not reorder the LDAP servers.')
    await reloadServers()
  }
}

function destroyTree() {
  if (el.value && onContextMenu) {
    el.value.removeEventListener('contextmenu', onContextMenu)
    onContextMenu = null
  }
  if (el.value && onExpanderDown) {
    el.value.removeEventListener('mousedown', onExpanderDown, true)
    onExpanderDown = null
  }
  if (onExpanderUp) {
    document.removeEventListener('mouseup', onExpanderUp, true)
    onExpanderUp = null
  }
  expanderPress = null
  if (tree) {
    tree.resizeObserver?.disconnect()
    tree = null
  }
}

function closeMenu() {
  menu.value = { ...menu.value, open: false }
}

function bindContextMenu() {
  if (!el.value || onContextMenu) return
  onContextMenu = (ev) => {
    ev.preventDefault()
    const wb = Wunderbaum.getNode(ev)
    const node = wb ? payload(wb) : null
    if (wb) wb.setActive()
    const x = Math.max(8, Math.min(ev.clientX, window.innerWidth - 220))
    const y = Math.max(8, Math.min(ev.clientY, window.innerHeight - 220))
    menu.value = { open: true, x, y, node }
  }
  el.value.addEventListener('contextmenu', onContextMenu)
}

// The name cell is draggable. Chrome then drops a click when the pointer
// moves a couple of pixels between press and release, which is what happens
// on the 14px +/− control. Toggle on mouseup instead, and cancel the press
// so that gesture does not start a drag.
function bindExpander() {
  if (!el.value || onExpanderDown) return
  onExpanderDown = (ev) => {
    if (ev.button !== 0 || !expanderFromEvent(ev)) return
    const node = Wunderbaum.getNode(ev)
    if (!isBranch(node)) return
    ev.preventDefault()
    ev.stopPropagation()
    expanderPress = { node, x: ev.clientX, y: ev.clientY }
  }
  onExpanderUp = (ev) => {
    const press = expanderPress
    expanderPress = null
    if (!press || ev.button !== 0) return
    const dx = ev.clientX - press.x
    const dy = ev.clientY - press.y
    if (dx * dx + dy * dy > 12 * 12) return
    press.node.setExpanded(!press.node.isExpanded())
  }
  el.value.addEventListener('mousedown', onExpanderDown, true)
  document.addEventListener('mouseup', onExpanderUp, true)
}

function buildTree() {
  if (!el.value) return
  destroyTree()
  const source = [internalBranch(), ...servers.value.map(serverBranch)]
  tree = new Wunderbaum({
    element: el.value,
    id: 'directory-tree',
    header: true,
    debugLevel: 0,
    rowHeightPx: 28,
    navigationModeOption: 'row',
    iconMap: {
      ...Wunderbaum.iconMaps?.bootstrap,
      expanderExpanded: '<i class="wb-expander">−</i>',
      expanderCollapsed: '<i class="wb-expander">+</i>',
      expanderLazy: '<i class="wb-expander">+</i>',
    },
    source,
    columns: [
      { id: '*', title: 'Name', width: '*' },
      { id: 'kind', title: 'Kind', width: '72px' },
    ],
    types: {
      user: { icon: false },
      role: { icon: false },
      internal: { icon: false },
      ou: { icon: false },
      server: { icon: false },
      directory: { icon: false },
      container: { icon: false },
      person: { icon: false },
      group: { icon: false },
      other: { icon: false },
    },
    lazyLoad: (e) => {
      const data = e.node.data ?? {}
      const serverId = data.server_id
      const dn = data.kind === 'server' ? data.dn || '' : (data.dn ?? '')
      return browseLdap(dn, serverId).then((rows) =>
        (rows ?? []).map((row) => toServerNode(row, serverId)),
      )
    },
    render: renderCell,
    // mouseup already toggled. Returning false keeps the grid click from
    // toggling the folder a second time.
    click: (e) => {
      if (e.event && expanderFromEvent(e.event)) return false
    },
    beforeActivate: (e) => {
      if (allowActivate || !formDirty()) return
      confirmDiscard().then((ok) => {
        if (!ok) return
        mark()
        creating.value = false
        allowActivate = true
        e.node.setActive(true).finally(() => {
          allowActivate = false
        })
      })
      return false
    },
    activate: (e) => {
      creating.value = false
      selected.value = payload(e.node)
      showSelection(selected.value)
    },
    dnd: {
      effectAllowed: 'move',
      dropEffectDefault: 'move',
      guessDropEffect: false,
      // Server rows stay lazy until they are opened. Reordering still has
      // to land on a server that has not been loaded.
      preventLazyParents: false,
      preventForeignNodes: true,
      preventNonNodes: true,
      dragStart: (e) => {
        const kind = nodeKind(e.node)
        return kind === 'user' || kind === 'role' || kind === 'ou' || kind === 'server'
      },
      dragEnter: (e) => {
        const source = e.sourceNode
        const target = e.node
        if (!source || !target) return false
        if (nodeKind(source) === 'server') return serverDropRegions(nodeKind(target))
        return internalRegions(source, target)
      },
      drop: (e) => onTreeDrop(e),
    },
  })
  bindContextMenu()
  bindExpander()
  applySearch()
}

function syncInternal() {
  if (!tree) {
    buildTree()
    return
  }
  const node = tree.findKey('internal')
  if (!node) return
  // removeChildren collapses the folder. Put the rows back and reopen it
  // when it was open, so a save does not hide the entry that was just added.
  // Rebuild would construct a second Wunderbaum on this host. The first one
  // already wrote the header, and that constructor then throws.
  const branch = internalBranch()
  const open = node.expanded !== false
  node.removeChildren()
  node.addChildren(branch.children)
  node.setTitle(branch.title)
  if (node.data) node.data.dn = branch.dn || ''
  if (open) node.setExpanded(true)
  applySearch()
}

async function refreshLdap(serverId, parentDN) {
  if (!tree || !serverId) return
  const node = tree.findKey(ldapNodeKey(serverId, parentDN))
  if (!node) return
  node.resetLazy()
  await node.setExpanded(true)
  applySearch()
}

function syncServerRoots() {
  if (!tree?.root) {
    buildTree()
    return
  }
  const open = new Map()
  for (const child of tree.root.children ?? []) {
    if (child.data?.kind === 'server' && child.data.server_id) {
      open.set(child.data.server_id, !!child.expanded)
    }
  }
  for (const child of [...(tree.root.children ?? [])]) {
    if (child.data?.kind === 'server') child.remove()
  }
  if (servers.value.length) tree.root.addChildren(servers.value.map(serverBranch))
  for (const s of servers.value) {
    const node = tree.findKey(`ldap-server-${s.id}`)
    if (node && open.get(s.id)) node.setExpanded(true)
  }
  applySearch()
}

function applySearch() {
  if (!tree) return
  const words = searchWords(search.value)
  if (!words.length) {
    tree.clearFilter()
    return
  }
  tree.filterNodes(
    (node) => {
      const data = node.data ?? {}
      const text = [
        node.title,
        data.kind,
        data.dn,
        data.email,
        data.name,
        data.username,
        data.description,
      ]
        .filter(Boolean)
        .join(' ')
        .toLowerCase()
      return words.every((w) => text.includes(w))
    },
    { mode: 'hide', autoExpand: true },
  )
}

// parentOfDN returns the DN above dn. Commas inside an escaped RDN value
// (uid=a\,b) are not separators.
function parentOfDN(dn) {
  let escaped = false
  for (let i = 0; i < (dn || '').length; i++) {
    const c = dn[i]
    if (escaped) {
      escaped = false
      continue
    }
    if (c === '\\') {
      escaped = true
      continue
    }
    if (c === ',') return dn.slice(i + 1).trim()
  }
  return ''
}

function ldapParentDN(node) {
  if (!node) return ''
  if (
    node.kind === 'server' ||
    node.kind === 'directory' ||
    node.kind === 'container' ||
    node.kind === 'other'
  ) {
    return node.dn || ''
  }
  if (node.kind === 'person' || node.kind === 'group') {
    return parentOfDN(node.dn || '') || serverOf(node.server_id)?.base_dn || ''
  }
  return ''
}

function roleNames(row) {
  return (row.role_ids ?? [])
    .map((id) => roles.value.find((r) => r.id === id)?.name)
    .filter(Boolean)
}

function isAdmin(row) {
  return roleNames(row).includes('admin')
}

async function guardEdits() {
  if (!formDirty()) return true
  const ok = await confirmDiscard()
  if (!ok) return false
  mark()
  return true
}

async function showSelection(n) {
  const seq = ++fillSeq
  submitted.value = false
  creating.value = false
  if (n?.kind === 'internal') {
    panel.value = 'internal'
    internalForm.value = {
      enabled: !!internal.value.enabled,
      listen: internal.value.listen || '',
      tls_listen: internal.value.tls_listen || '',
      tls_certificate_id: internal.value.tls_certificate_id || 0,
      base_dn: internal.value.base_dn || '',
      radius_use_internal: !!internal.value.radius_use_internal,
      bind_password: '',
    }
    await nextTick()
    if (seq === fillSeq) mark()
    return
  }
  if (n?.kind === 'ou') {
    panel.value = 'ou'
    ouForm.value = { id: n.ou_id, name: n.name || n.title, parent_id: n.parent_id || 0 }
    await nextTick()
    if (seq === fillSeq) mark()
    return
  }
  if (n?.kind === 'server') {
    panel.value = 'server'
    const row = serverOf(n.server_id) || {}
    serverForm.value = serverFormFrom(row)
    await nextTick()
    if (seq === fillSeq) mark()
    return
  }
  if (!n || !['user', 'role', 'person', 'group'].includes(n.kind)) {
    panel.value = n ? 'node' : ''
    await nextTick()
    if (seq === fillSeq) mark()
    return
  }
  if (n.kind === 'user') {
    panel.value = 'user'
    user.value = {
      id: n.user_id,
      username: n.username,
      name: n.name,
      email: n.email,
      mobile: n.mobile,
      role_ids: [...(n.role_ids ?? [])],
      ou_id: ouOf('user', n.user_id),
      password: '',
    }
  } else if (n.kind === 'role') {
    panel.value = 'role'
    role.value = {
      id: n.role_id,
      name: n.name,
      description: n.description || '',
      ou_id: ouOf('role', n.role_id),
      member_role_ids: memberRoleIDs(n.role_id),
    }
  } else if (n.kind === 'person') {
    panel.value = 'person'
    personLoading.value = true
    person.value = { dn: n.dn || n.key, editable: false, password: '' }
    try {
      const entry = await getLdapEntry(n.dn || n.key, n.server_id)
      if (seq !== fillSeq) return
      person.value = { ...entry, server_id: n.server_id, password: '' }
    } catch (err) {
      if (seq !== fillSeq) return
      panel.value = ''
      toastError(err, 'Failed to load the directory user.')
    } finally {
      personLoading.value = false
    }
  } else if (n.kind === 'group') {
    panel.value = 'group'
    groupLoading.value = true
    group.value = { dn: n.dn || n.key, members: [], editable: false }
    try {
      const entry = await getLdapEntry(n.dn || n.key, n.server_id)
      if (seq !== fillSeq) return
      group.value = { ...entry, server_id: n.server_id, members: entry.members ?? [] }
    } catch (err) {
      if (seq !== fillSeq) return
      panel.value = ''
      toastError(err, 'Failed to load the directory group.')
    } finally {
      groupLoading.value = false
    }
  }
  await nextTick()
  if (seq === fillSeq) mark()
}

async function activateKey(key) {
  const node = key ? tree?.findKey(key) : null
  if (!node) {
    selected.value = null
    panel.value = ''
    await nextTick()
    mark()
    return
  }
  const same = tree.getActiveNode?.() === node
  allowActivate = true
  try {
    await node.setActive(true)
  } finally {
    allowActivate = false
  }
  if (same) {
    selected.value = payload(node)
    await showSelection(selected.value)
  }
}

async function openLocalUser() {
  if (!(await guardEdits())) return
  creating.value = true
  submitted.value = false
  panel.value = 'user'
  user.value = { role_ids: [], ou_id: 0 }
  await nextTick()
  mark()
}

async function openUserIn(ouId) {
  await openLocalUser()
  user.value.ou_id = ouId || 0
  await nextTick()
  mark()
}

async function openNewRole() {
  if (!(await guardEdits())) return
  creating.value = true
  submitted.value = false
  panel.value = 'role'
  role.value = { ou_id: 0, member_role_ids: [] }
  await nextTick()
  mark()
}

async function openRoleIn(ouId) {
  await openNewRole()
  role.value.ou_id = ouId || 0
  await nextTick()
  mark()
}

async function openNewFolder(node) {
  if (!(await guardEdits())) return
  creating.value = true
  submitted.value = false
  panel.value = 'ou'
  ouForm.value = { name: '', parent_id: node?.kind === 'ou' ? node.ou_id : 0 }
  await nextTick()
  mark()
}

async function openNewPerson(node) {
  if (!(await guardEdits())) return
  creating.value = true
  submitted.value = false
  panel.value = 'person'
  person.value = {
    server_id: node?.server_id,
    parent_dn: ldapParentDN(node),
    uid: '',
    cn: '',
    sn: '',
    mail: '',
    display_name: '',
    mobile: '',
    password: '',
    editable: true,
  }
  await nextTick()
  mark()
}

async function openNewGroup(node) {
  if (!(await guardEdits())) return
  const target = node || selected.value
  const parent = ldapParentDN(target)
  creating.value = true
  submitted.value = false
  panel.value = 'group'
  group.value = {
    server_id: target?.server_id,
    parent_dn: parent,
    cn: '',
    description: '',
    members: [],
    editable: true,
  }
  await nextTick()
  mark()
}

function openNewUser() {
  if (
    nodeWritable(selected.value) &&
    ['server', 'directory', 'container', 'other', 'person', 'group'].includes(selectedKind.value)
  ) {
    return openNewPerson(selected.value)
  }
  return openLocalUser()
}

function saveUser() {
  submitted.value = true
  if (!user.value.username?.trim() || (!user.value.id && !user.value.password)) return
  userSaving.value = true
  const body = { ...user.value }
  if (!body.password) delete body.password
  const request = body.id ? updateUser(body.id, body) : createUser(body)
  request
    .then(async (saved) => {
      const id = saved?.id || body.id
      await placeLdap({ kind: 'user', id, ou_id: user.value.ou_id || null })
      toastOk(body.id ? 'User updated' : 'User created')
      creating.value = false
      mark()
      await reloadLocal()
      await activateKey(`dir-user-${id}`)
    })
    .catch((err) => toastError(err, 'Failed to save user.'))
    .finally(() => {
      userSaving.value = false
    })
}

async function performDeleteUser(target) {
  const row = target || user.value
  const id = row?.id || row?.user_id
  if (!id || isAdmin(row)) return
  if (!(await confirmDelete(`user ${row.username}`))) return
  userDeleting.value = true
  deleteUser(id)
    .then(async () => {
      toastOk('User deleted')
      mark()
      if ((user.value.id || user.value.user_id) === id) panel.value = ''
      await reloadLocal()
    })
    .catch((err) => toastError(err, 'Failed to delete user.'))
    .finally(() => {
      userDeleting.value = false
    })
}

function saveRole() {
  submitted.value = true
  if (!role.value.name?.trim()) return
  roleSaving.value = true
  const body = { ...role.value }
  const request = body.id ? updateRole(body.id, body) : createRole(body)
  request
    .then(async (saved) => {
      const id = saved?.id || body.id
      await placeLdap({ kind: 'role', id, ou_id: role.value.ou_id || null })
      await setRoleMembers(id, role.value.member_role_ids || [])
      toastOk(body.id ? 'Role updated' : 'Role created')
      creating.value = false
      mark()
      await reloadLocal()
      await activateKey(`dir-role-${id}`)
    })
    .catch((err) => toastError(err, 'Failed to save role.'))
    .finally(() => {
      roleSaving.value = false
    })
}

function personParent() {
  if (person.value.dn) return parentOfDN(person.value.dn)
  return person.value.parent_dn || ''
}

function savePerson() {
  submitted.value = true
  if (!person.value.editable) return
  if (!person.value.uid?.trim() || !person.value.cn?.trim() || !person.value.sn?.trim()) return
  if (!person.value.dn && !person.value.password) return
  personSaving.value = true
  const body = {
    server_id: person.value.server_id,
    dn: person.value.dn,
    parent_dn: person.value.parent_dn,
    uid: person.value.uid,
    cn: person.value.cn,
    sn: person.value.sn,
    mail: person.value.mail,
    display_name: person.value.display_name,
    mobile: person.value.mobile,
    password: person.value.password,
  }
  if (!body.password) delete body.password
  const request = body.dn ? updateLdapPerson(body) : createLdapPerson(body)
  const parent = personParent()
  request
    .then(async (saved) => {
      toastOk(body.dn ? 'Directory user updated' : 'Directory user created')
      creating.value = false
      mark()
      await refreshLdap(body.server_id, parent)
      await activateKey(ldapNodeKey(body.server_id, saved?.dn || body.dn))
    })
    .catch((err) => toastError(err, 'Failed to save the directory user.'))
    .finally(() => {
      personSaving.value = false
    })
}

async function performDeletePerson(target) {
  const row = target?.dn ? target : person.value
  if (!row?.dn) return
  if (row.editable === false) return
  const label = row.uid || row.dn
  if (
    !(await confirmDelete(
      `directory user ${label}`,
      'The local Factum account, if one was created at login, is left in place.',
    ))
  ) {
    return
  }
  personDeleting.value = true
  const parent = parentOfDN(row.dn)
  const serverId = row.server_id || person.value.server_id
  deleteLdapPerson(row.dn, serverId)
    .then(async (res) => {
      mark()
      if (person.value.dn === row.dn) panel.value = ''
      await refreshLdap(serverId, parent)
      if (res?.local_user_id) {
        const also = await ask({
          title: 'Local account',
          message: `A local account for ${res.local_username} is still in Factum. Delete that too?`,
        })
        if (also) {
          await deleteUser(res.local_user_id)
          await reloadLocal()
        }
      }
      toastOk('Directory user deleted')
    })
    .catch((err) => toastError(err, 'Failed to delete the directory user.'))
    .finally(() => {
      personDeleting.value = false
    })
}

function addMember(dn) {
  const members = group.value.members ?? []
  if (!members.some((m) => m.toLowerCase() === dn.toLowerCase())) {
    group.value.members = [...members, dn]
  }
}

function removeMember(dn) {
  group.value.members = (group.value.members ?? []).filter((m) => m !== dn)
}

function saveGroup() {
  submitted.value = true
  if (!group.value.editable) return
  if (!group.value.cn?.trim() || !(group.value.members ?? []).length) return
  groupSaving.value = true
  const body = {
    server_id: group.value.server_id,
    dn: group.value.dn,
    parent_dn: group.value.parent_dn,
    cn: group.value.cn,
    description: group.value.description,
    members: group.value.members,
  }
  const parent = body.dn ? parentOfDN(body.dn) : body.parent_dn
  const request = body.dn ? updateLdapGroup(body) : createLdapGroup(body)
  request
    .then(async (saved) => {
      toastOk(body.dn ? 'Directory group updated' : 'Directory group created')
      creating.value = false
      mark()
      await refreshLdap(body.server_id, parent)
      await activateKey(ldapNodeKey(body.server_id, saved?.dn || body.dn))
    })
    .catch((err) => toastError(err, 'Failed to save the directory group.'))
    .finally(() => {
      groupSaving.value = false
    })
}

async function performDeleteGroup(target) {
  const row = target?.dn ? target : group.value
  if (!row?.dn) return
  if (row.editable === false) return
  if (
    !(await confirmDelete(
      `directory group ${row.cn || row.title || row.dn}`,
      'Factum role mappings and RADIUS policies that point at this group are removed. Factum roles stay.',
    ))
  ) {
    return
  }
  groupDeleting.value = true
  const parent = parentOfDN(row.dn)
  const serverId = row.server_id || group.value.server_id
  deleteLdapGroup(row.dn, serverId)
    .then(async () => {
      toastOk('Directory group deleted')
      mark()
      if (group.value.dn === row.dn) panel.value = ''
      await refreshLdap(serverId, parent)
    })
    .catch((err) => toastError(err, 'Failed to delete the directory group.'))
    .finally(() => {
      groupDeleting.value = false
    })
}

function deleteNode(node) {
  if (!node) return
  if (node.kind === 'server') return performDeleteServer(node)
  if (node.kind === 'user') return performDeleteUser(node)
  if (node.kind === 'person') return performDeletePerson(node)
  if (node.kind === 'group') return performDeleteGroup(node)
  if (node.kind === 'ou') return performDeleteOU(node)
}

async function performDeleteOU(node) {
  const id = node?.ou_id || ouForm.value.id
  const name = node?.name || node?.title || ouForm.value.name
  if (!id) return
  if (!(await confirmDelete(`folder ${name}`))) return
  deleteLdapOU(id)
    .then(async () => {
      toastOk('Folder deleted')
      mark()
      if (ouForm.value.id === id) panel.value = ''
      await reloadInternal()
    })
    .catch((err) => toastError(err, 'Failed to delete the folder.'))
}

function saveOU() {
  submitted.value = true
  if (!ouForm.value.name?.trim()) return
  const body = {
    name: ouForm.value.name.trim(),
    parent_id: ouForm.value.parent_id || null,
  }
  const request = ouForm.value.id ? updateLdapOU(ouForm.value.id, body) : createLdapOU(body)
  request
    .then(async (saved) => {
      toastOk(ouForm.value.id ? 'Folder updated' : 'Folder created')
      creating.value = false
      mark()
      await reloadInternal()
      await activateKey(`ou-${saved?.id || ouForm.value.id}`)
    })
    .catch((err) => toastError(err, 'Failed to save the folder.'))
}

function saveInternal() {
  const body = {
    enabled: !!internalForm.value.enabled,
    listen: internalForm.value.listen || '',
    tls_listen: internalForm.value.tls_listen || '',
    tls_certificate_id: internalForm.value.tls_certificate_id || null,
    base_dn: internalForm.value.base_dn || '',
    radius_use_internal: !!internalForm.value.radius_use_internal,
  }
  if (internalForm.value.bind_password) body.bind_password = internalForm.value.bind_password
  updateLdapInternal(body)
    .then(async (saved) => {
      internal.value = saved
      internalForm.value.bind_password = ''
      toastOk('Directory saved')
      mark()
      await nextTick()
      syncInternal()
      await activateKey('internal')
    })
    .catch((err) => toastError(err, 'Failed to save the directory.'))
}

function serverFormFrom(row) {
  const ad = (row.server_type || 'generic') === 'ad'
  return {
    id: row.id,
    enabled: row.enabled !== false,
    name: row.name || '',
    server_type: row.server_type || 'generic',
    host: row.host || '',
    port: row.port || 389,
    tls_mode: row.tls_mode || 'starttls',
    skip_tls_verify: !!row.skip_tls_verify,
    bind_mode: (row.bind_dn || '').trim() ? 'service' : row.id ? 'anonymous' : 'service',
    bind_dn: row.bind_dn || '',
    bind_password: '',
    bind_password_set: !!row.bind_password_set,
    base_dn: row.base_dn || '',
    user_filter: row.user_filter || (ad ? '(sAMAccountName=%s)' : '(uid=%s)'),
    attr_username: row.attr_username || '',
    attr_email: row.attr_email || '',
    attr_display_name: row.attr_display_name || '',
    attr_mobile: row.attr_mobile || '',
    attr_groups: row.attr_groups || '',
    allow_password_change: !!row.allow_password_change,
    position: row.position > 0 ? row.position : null,
  }
}

async function openNewServer(serverType) {
  if (!(await guardEdits())) return
  creating.value = true
  submitted.value = false
  panel.value = 'server'
  serverForm.value = serverFormFrom({
    server_type: serverType,
    enabled: true,
    tls_mode: 'starttls',
  })
  await nextTick()
  mark()
}

function saveServer() {
  submitted.value = true
  if (!serverForm.value.host?.trim()) return
  const body = { ...serverForm.value }
  if (body.bind_mode === 'anonymous') {
    body.bind_dn = ''
    body.bind_password = ''
  }
  delete body.bind_mode
  delete body.bind_password_set
  delete body.writable
  if (!body.bind_password) delete body.bind_password
  if (!(Number(body.position) > 0)) delete body.position
  serverSaving.value = true
  const request = body.id ? updateLdapServer(body.id, body) : createLdapServer(body)
  request
    .then(async (saved) => {
      toastOk(body.id ? 'LDAP server updated' : 'LDAP server created')
      creating.value = false
      mark()
      await reloadServers()
      await activateKey(`ldap-server-${saved?.id || body.id}`)
    })
    .catch((err) => toastError(err, 'Failed to save the LDAP server.'))
    .finally(() => {
      serverSaving.value = false
    })
}

async function performDeleteServer(node) {
  const id = node?.server_id || serverForm.value.id
  const row = serverOf(id) || serverForm.value
  const label = serverTitle(row) || 'LDAP server'
  if (!id) return
  if (!(await confirmDelete(`LDAP server ${label}`))) return
  serverDeleting.value = true
  deleteLdapServer(id)
    .then(async () => {
      toastOk('LDAP server deleted')
      const closed = serverForm.value.id === id
      if (closed) panel.value = ''
      await reloadServers()
      await nextTick()
      // Mark after the panel is gone. Marking the open form first makes
      // the empty panel look like an unsaved edit and blocks the next click.
      if (closed) mark()
    })
    .catch((err) => toastError(err, 'Failed to delete the LDAP server.'))
    .finally(() => {
      serverDeleting.value = false
    })
}

function testServer() {
  const form = serverForm.value
  serverTesting.value = true
  const payload = {
    id: form.id,
    ldap_host: form.host,
    ldap_port: form.port,
    ldap_tls_mode: form.tls_mode,
    ldap_skip_tls_verify: !!form.skip_tls_verify,
    ldap_bind_dn: form.bind_mode === 'anonymous' ? '' : form.bind_dn,
    ldap_bind_password: form.bind_mode === 'anonymous' ? '' : form.bind_password,
    ldap_base_dn: form.base_dn,
  }
  testLdapConnection(payload)
    .then((data) => {
      if (data.ok) {
        toastOk('Bound to the LDAP server.')
        return
      }
      toast.add({
        color: 'error',
        title: 'Connection failed',
        description: data.error || 'The server did not accept the bind.',
        duration: 6000,
      })
    })
    .catch((err) => toastError(err, 'Failed to test the connection.'))
    .finally(() => {
      serverTesting.value = false
    })
}

function reloadServers() {
  return getLdapServers().then((rows) => {
    servers.value = rows ?? []
    syncServerRoots()
  })
}

function runMenu(id) {
  const node = menu.value.node
  closeMenu()
  if (id === 'new-openldap') return openNewServer('generic')
  if (id === 'new-ad') return openNewServer('ad')
  if (id === 'new-folder') return openNewFolder(node)
  if (id === 'new-user' && (node?.kind === 'ou' || node?.kind === 'internal')) {
    return openUserIn(node.kind === 'ou' ? node.ou_id : 0)
  }
  if (id === 'new-role' && (node?.kind === 'ou' || node?.kind === 'internal')) {
    return openRoleIn(node.kind === 'ou' ? node.ou_id : 0)
  }
  if (id === 'new-user') return openLocalUser()
  if (id === 'new-role') return openNewRole()
  if (id === 'new-person') return openNewPerson(node)
  if (id === 'new-group') return openNewGroup(node)
  if (id === 'edit') return showSelection(node)
  if (id === 'delete') return deleteNode(node)
}

function onDocClick() {
  if (menu.value.open) closeMenu()
}

function reloadLocal() {
  return Promise.all([getUsers(), getRoles(), getLdapInternal()]).then(
    ([userRows, roleRows, dir]) => {
      users.value = userRows ?? []
      roles.value = roleRows ?? []
      internal.value = dir ?? internal.value
      syncInternal()
    },
  )
}

function reloadInternal() {
  return getLdapInternal().then((dir) => {
    internal.value = dir ?? internal.value
    syncInternal()
  })
}

function load() {
  loading.value = true
  error.value = null
  forbidden.value = false
  Promise.all([getUsers(), getRoles(), getLdapServers(), getLdapInternal()])
    .then(([userRows, roleRows, serverRows, dir]) => {
      users.value = userRows ?? []
      roles.value = roleRows ?? []
      servers.value = serverRows ?? []
      internal.value = dir ?? internal.value
    })
    .catch((err) => {
      if (err.response?.status === 403 || err.response?.status === 401) {
        forbidden.value = true
      } else {
        error.value = errMsg(err, 'Failed to load the directory.')
      }
    })
    .finally(() => {
      loading.value = false
      nextTick(() => {
        if (!forbidden.value) buildTree()
      })
    })
}

watch(search, applySearch)

// The tree is built once. Opening this page on Authorization leaves the host
// at zero size, so lay it out again when Directory is shown.
watch(tab, (value) => {
  if (value !== 'directory' || forbidden.value || loading.value) return
  nextTick(() => {
    if (!el.value) return
    if (!tree) buildTree()
    else tree.update('resize', { immediate: true })
  })
})

onMounted(() => {
  document.addEventListener('click', onDocClick)
  load()
})
onBeforeUnmount(() => {
  document.removeEventListener('click', onDocClick)
  destroyTree()
})
</script>

<template>
  <div v-if="forbidden" class="card">
    <UAlert
      color="error"
      variant="subtle"
      title="You need administrator permissions to view the directory."
    />
  </div>
  <UTabs
    v-else
    v-model="tab"
    :items="directoryTabs"
    :unmount-on-hide="false"
    class="min-h-0 flex-1"
    :ui="{
      root: 'min-h-0 flex-1 items-stretch',
      list: 'shrink-0',
      content: 'flex min-h-0 flex-1 flex-col overflow-auto focus-visible:outline-none',
    }"
  >
    <template #directory>
      <div class="card flex min-h-0 flex-1 flex-col">
        <PageFormBar>
          <template #title>
            <div>
              <div>Directory</div>
              <small class="font-normal text-muted-color">{{ intro }}</small>
            </div>
          </template>
          <UButton
            label="New folder"
            icon="i-lucide-folder-plus"
            color="neutral"
            size="sm"
            @click="openNewFolder(selected?.kind === 'ou' ? selected : null)"
          />
          <UButton
            label="New user"
            icon="i-lucide-plus"
            color="neutral"
            size="sm"
            @click="openNewUser"
          />
          <UButton
            label="New role"
            icon="i-lucide-plus"
            color="neutral"
            size="sm"
            @click="openNewRole"
          />
          <UButton
            label="OpenLDAP"
            icon="i-lucide-plus"
            color="neutral"
            size="sm"
            @click="openNewServer('generic')"
          />
          <UButton
            label="Active Directory"
            icon="i-lucide-plus"
            color="neutral"
            size="sm"
            @click="openNewServer('ad')"
          />
          <UButton
            v-if="nodeWritable(selected)"
            label="New group"
            icon="i-lucide-plus"
            color="neutral"
            size="sm"
            @click="openNewGroup()"
          />
          <UButton
            v-if="directoryCanSave"
            label="Save"
            icon="i-lucide-check"
            type="submit"
            :form="directoryFormId"
            :loading="directorySaveLoading"
          />
        </PageFormBar>
        <UAlert v-if="error" color="error" variant="subtle" :title="error" class="mb-3" />
        <div class="flex min-h-0 flex-1 flex-col gap-4 overflow-hidden lg:flex-row">
          <div class="flex min-h-0 w-full min-w-0 flex-1 flex-col lg:w-96 lg:flex-none">
            <SearchInput v-model="search" class="mb-3 shrink-0" />
            <div class="directory-tree-host">
              <div ref="el" class="ipam-tree" />
            </div>
            <p v-if="loading" class="mt-2 shrink-0 text-sm text-muted-color">Loading…</p>
          </div>
          <div class="min-h-0 min-w-0 flex-1 overflow-auto">
            <p v-if="!panel" class="text-sm text-muted-color">
              Select an entry. Drag a user, role, or folder onto a folder. Drag an LDAP or Active
              Directory server to change the login order. The local directory stays first.
              Right-click a row to add, change, or remove it.
            </p>
            <form
              v-else-if="panel === 'server'"
              id="directory-server-form"
              class="flex flex-col gap-4"
              @submit.prevent="saveServer"
            >
              <h5 class="m-0">{{ serverForm.id ? serverTitle(serverForm) : 'New LDAP server' }}</h5>
              <p class="text-sm text-muted-color">
                Login tries enabled servers from lowest order to highest. A server that is down or
                rejects the password is skipped. If none accepts the client, access is denied.
              </p>
              <UCheckbox v-model="serverForm.enabled" label="Enabled" />
              <UFormField label="Name">
                <UInput v-model="serverForm.name" placeholder="dc1" class="w-full" />
                <small class="text-muted-color"
                  >Shown in the tree. The host is used when this is empty.</small
                >
              </UFormField>
              <UFormField label="Order">
                <UInputNumber
                  v-model="serverForm.position"
                  :min="1"
                  :format-options="{ useGrouping: false }"
                  class="w-full"
                />
                <small class="text-muted-color"
                  >Lower numbers are tried first. Drag the server in the tree to change this. The
                  local directory stays first. Empty puts a new server last.</small
                >
              </UFormField>
              <UFormField label="Server type">
                <URadioGroup v-model="serverForm.server_type" :items="serverTypeOptions" />
              </UFormField>
              <UFormField label="Host">
                <UInput
                  v-model="serverForm.host"
                  placeholder="dc1.example.com"
                  :color="submitted && !serverForm.host?.trim() ? 'error' : undefined"
                  :highlight="submitted && !serverForm.host?.trim()"
                  class="w-full"
                />
              </UFormField>
              <UFormField label="Port">
                <UInputNumber
                  v-model="serverForm.port"
                  :format-options="{ useGrouping: false }"
                  class="w-full"
                />
              </UFormField>
              <UFormField label="TLS">
                <USelect v-model="serverForm.tls_mode" :items="tlsModeOptions" class="w-full" />
              </UFormField>
              <UCheckbox
                v-model="serverForm.skip_tls_verify"
                label="Skip TLS certificate verification"
              />
              <UFormField label="Bind">
                <URadioGroup v-model="serverForm.bind_mode" :items="bindModeOptions" />
                <small v-if="serverForm.bind_mode === 'anonymous'" class="text-muted-color">
                  The directory must allow anonymous search. Active Directory usually does not.
                </small>
              </UFormField>
              <UFormField label="Bind DN">
                <UInput
                  v-model="serverForm.bind_dn"
                  :disabled="serverForm.bind_mode === 'anonymous'"
                  placeholder="cn=svc-factum,dc=example,dc=com"
                  class="w-full font-mono"
                />
              </UFormField>
              <UFormField label="Bind password">
                <PasswordInput
                  v-model="serverForm.bind_password"
                  :disabled="serverForm.bind_mode === 'anonymous'"
                />
                <small class="text-muted-color">
                  {{
                    serverForm.bind_password_set
                      ? 'Leave blank to keep the current password.'
                      : 'Used to search for the user before the password bind.'
                  }}
                </small>
              </UFormField>
              <UFormField label="Base DN">
                <UInput
                  v-model="serverForm.base_dn"
                  placeholder="dc=example,dc=com"
                  class="w-full font-mono"
                />
              </UFormField>
              <UFormField label="User filter">
                <UInput
                  v-model="serverForm.user_filter"
                  :placeholder="
                    serverForm.server_type === 'ad' ? '(sAMAccountName=%s)' : '(uid=%s)'
                  "
                  class="w-full font-mono"
                />
                <small class="text-muted-color">One %s for the escaped username.</small>
              </UFormField>
              <UFormField label="Username attribute">
                <UInput
                  v-model="serverForm.attr_username"
                  :placeholder="serverForm.server_type === 'ad' ? 'sAMAccountName' : 'uid'"
                  class="w-full"
                />
                <small class="text-muted-color"
                  >Used when a password reset looks the account up by email.</small
                >
              </UFormField>
              <UFormField label="Email attribute">
                <UInput v-model="serverForm.attr_email" placeholder="mail" class="w-full" />
              </UFormField>
              <UFormField label="Display name attribute">
                <UInput
                  v-model="serverForm.attr_display_name"
                  placeholder="displayName"
                  class="w-full"
                />
              </UFormField>
              <UFormField label="Mobile attribute">
                <UInput v-model="serverForm.attr_mobile" placeholder="mobile" class="w-full" />
              </UFormField>
              <UFormField label="Groups attribute">
                <UInput v-model="serverForm.attr_groups" placeholder="memberOf" class="w-full" />
              </UFormField>
              <UCheckbox
                v-model="serverForm.allow_password_change"
                label="Allow password changes to be written back"
              />
              <p class="text-sm text-muted-color">
                The account that writes the password is ldap_writeback in the config file. The same
                account adds and changes OpenLDAP entries. Active Directory stays browse-only.
              </p>
              <div class="flex gap-2">
                <UButton
                  label="Test connection"
                  icon="i-lucide-zap"
                  color="neutral"
                  type="button"
                  :loading="serverTesting"
                  @click="testServer"
                />
                <UButton
                  v-if="serverForm.id"
                  label="Delete"
                  icon="i-lucide-trash"
                  color="error"
                  variant="ghost"
                  type="button"
                  :loading="serverDeleting"
                  @click="performDeleteServer()"
                />
              </div>
            </form>

            <form
              v-else-if="panel === 'internal'"
              id="directory-internal-form"
              class="flex flex-col gap-4"
              @submit.prevent="saveInternal"
            >
              <h5 class="m-0">Internal directory</h5>
              <p class="text-sm text-muted-color">
                Workers with an <span class="font-mono">ldap</span> command serve this directory.
                Put people and roles in folders. A role can contain other roles; membership is
                expanded the way Active Directory expands nested groups.
              </p>
              <UCheckbox v-model="internalForm.enabled" label="Enable" />
              <UFormField label="Listen">
                <UInput v-model="internalForm.listen" placeholder=":389" class="w-full" />
                <small class="text-muted-color">Empty uses :389. host:port.</small>
              </UFormField>
              <UFormField label="LDAPS listen">
                <UInput v-model="internalForm.tls_listen" placeholder=":636" class="w-full" />
                <small class="text-muted-color">Optional. host:port. Empty leaves LDAPS off.</small>
              </UFormField>
              <UFormField label="LDAPS certificate">
                <USelectMenu
                  v-model="internalForm.tls_certificate_id"
                  :items="ldapsCertItems"
                  value-key="value"
                  label-key="label"
                  class="w-full"
                />
                <small class="text-muted-color">
                  A certificate from Services → Certificates. The worker reads it from lego storage.
                  Worker hub certificate uses the hub TLS pair.
                </small>
              </UFormField>
              <UFormField label="Base DN">
                <UInput
                  v-model="internalForm.base_dn"
                  placeholder="dc=factum,dc=local"
                  class="w-full"
                />
              </UFormField>
              <UFormField label="Search bind DN">
                <UInput :model-value="internal.bind_dn" readonly class="w-full font-mono" />
              </UFormField>
              <UFormField label="Search password">
                <PasswordInput v-model="internalForm.bind_password" />
                <small class="text-muted-color">
                  {{
                    internal.bind_password_set
                      ? 'Leave blank to keep the current password.'
                      : 'NetBox, LibreNMS, and Icinga bind with this password before they search.'
                  }}
                </small>
              </UFormField>
              <UFormField label="User filter">
                <UInput
                  :model-value="internal.user_filter || '(uid=%s)'"
                  readonly
                  class="w-full font-mono"
                />
              </UFormField>
              <UCheckbox
                v-model="internalForm.radius_use_internal"
                label="RADIUS on the same worker uses this directory"
              />
              <p class="text-sm text-muted-color">
                RADIUS binds to 127.0.0.1 on the plain listen port. That worker needs both
                <span class="font-mono">radius</span> and <span class="font-mono">ldap</span>.
                MikroTik MS-CHAPv2 still needs Active Directory.
              </p>
            </form>

            <form
              v-else-if="panel === 'ou'"
              id="directory-ou-form"
              class="flex flex-col gap-4"
              @submit.prevent="saveOU"
            >
              <h5 class="m-0">{{ ouForm.id ? 'Folder' : 'New folder' }}</h5>
              <UFormField label="Name">
                <UInput
                  v-model.trim="ouForm.name"
                  :color="submitted && !ouForm.name?.trim() ? 'error' : undefined"
                  :highlight="submitted && !ouForm.name?.trim()"
                  autofocus
                  class="w-full"
                />
                <small v-if="submitted && !ouForm.name?.trim()" class="text-red-500"
                  >Name is required.</small
                >
              </UFormField>
              <UFormField label="Inside">
                <USelectMenu
                  v-model="ouForm.parent_id"
                  :items="ouParentItems"
                  value-key="value"
                  label-key="label"
                  class="w-full"
                />
              </UFormField>
              <p v-if="ouForm.id && selected?.dn" class="font-mono text-sm break-all">
                {{ selected.dn }}
              </p>
              <div class="flex gap-2">
                <UButton
                  v-if="ouForm.id"
                  label="Delete"
                  icon="i-lucide-trash"
                  color="error"
                  variant="ghost"
                  @click="performDeleteOU(selected)"
                />
              </div>
            </form>

            <div v-else-if="panel === 'node'" class="flex flex-col gap-2">
              <h5 class="m-0">{{ selected?.title }}</h5>
              <p class="text-sm text-muted-color">{{ ldapKindLabel(selected?.kind) }}</p>
              <p v-if="selected?.dn" class="font-mono text-sm break-all">{{ selected.dn }}</p>
            </div>
            <form
              v-else-if="panel === 'user'"
              id="directory-user-form"
              class="flex flex-col gap-4"
              @submit.prevent="saveUser"
            >
              <h5 class="m-0">{{ user.id ? 'User' : 'New user' }}</h5>
              <UFormField label="Username">
                <UInput
                  v-model.trim="user.username"
                  :color="submitted && !user.username?.trim() ? 'error' : undefined"
                  :highlight="submitted && !user.username?.trim()"
                  autofocus
                  class="w-full"
                />
                <small v-if="submitted && !user.username?.trim()" class="text-red-500"
                  >Username is required.</small
                >
              </UFormField>
              <UFormField label="Name">
                <UInput v-model="user.name" class="w-full" />
              </UFormField>
              <UFormField label="Email">
                <UInput v-model="user.email" class="w-full" />
              </UFormField>
              <UFormField label="Mobile">
                <UInput v-model="user.mobile" class="w-full" />
              </UFormField>
              <UFormField label="Roles">
                <USelectMenu
                  v-model="user.role_ids"
                  :items="roles"
                  value-key="id"
                  label-key="name"
                  placeholder="Select Roles"
                  multiple
                  class="w-full"
                />
              </UFormField>
              <UFormField label="Folder">
                <USelectMenu
                  v-model="user.ou_id"
                  :items="userFolderItems"
                  value-key="value"
                  label-key="label"
                  class="w-full"
                />
                <small v-if="!user.ou_id" class="text-muted-color">
                  Default publishes this person under ou=People.
                </small>
              </UFormField>
              <UFormField label="Password">
                <PasswordInput
                  v-model="user.password"
                  :color="submitted && !user.id && !user.password ? 'error' : undefined"
                  :highlight="submitted && !user.id && !user.password"
                />
                <small v-if="submitted && !user.id && !user.password" class="text-red-500"
                  >Password is required.</small
                >
                <small v-else-if="user.id" class="text-muted-color"
                  >Leave blank to keep the current password.</small
                >
              </UFormField>
              <div class="flex gap-2">
                <UButton
                  v-if="user.id && !isAdmin(user)"
                  label="Delete"
                  icon="i-lucide-trash"
                  color="error"
                  variant="ghost"
                  :loading="userDeleting"
                  @click="performDeleteUser()"
                />
              </div>
            </form>

            <form
              v-else-if="panel === 'role'"
              id="directory-role-form"
              class="flex flex-col gap-4"
              @submit.prevent="saveRole"
            >
              <h5 class="m-0">{{ role.id ? 'Role' : 'New role' }}</h5>
              <UFormField label="Name">
                <UInput
                  v-model.trim="role.name"
                  :color="submitted && !role.name?.trim() ? 'error' : undefined"
                  :highlight="submitted && !role.name?.trim()"
                  autofocus
                  class="w-full"
                />
                <small v-if="submitted && !role.name?.trim()" class="text-red-500"
                  >Name is required.</small
                >
              </UFormField>
              <UFormField label="Description">
                <UTextarea v-model="role.description" :rows="3" class="w-full" />
              </UFormField>
              <UFormField label="Folder">
                <USelectMenu
                  v-model="role.ou_id"
                  :items="roleFolderItems"
                  value-key="value"
                  label-key="label"
                  class="w-full"
                />
                <small v-if="!role.ou_id" class="text-muted-color">
                  Default publishes this role under ou=Roles.
                </small>
              </UFormField>
              <UFormField label="Contains roles">
                <USelectMenu
                  v-model="role.member_role_ids"
                  :items="nestRoleItems"
                  value-key="id"
                  label-key="name"
                  placeholder="Nested roles"
                  multiple
                  class="w-full"
                />
                <small class="text-muted-color">
                  People in a contained role are also in this role, including through further
                  levels.
                </small>
              </UFormField>
            </form>

            <form
              v-else-if="panel === 'person'"
              id="directory-person-form"
              class="flex flex-col gap-4"
              @submit.prevent="savePerson"
            >
              <h5 class="m-0">{{ person.dn ? 'Directory user' : 'New directory user' }}</h5>
              <p v-if="personLoading" class="text-sm text-muted-color">Loading…</p>
              <UFormField label="Container">
                <UInput :model-value="personParent()" readonly class="w-full" />
              </UFormField>
              <UFormField label="Username">
                <UInput
                  v-model.trim="person.uid"
                  :disabled="!person.editable"
                  :color="submitted && !person.uid?.trim() ? 'error' : undefined"
                  :highlight="submitted && !person.uid?.trim()"
                  autofocus
                  class="w-full"
                />
                <small v-if="submitted && !person.uid?.trim()" class="text-red-500"
                  >Username is required.</small
                >
              </UFormField>
              <UFormField label="Name">
                <UInput v-model="person.cn" :disabled="!person.editable" class="w-full" />
                <small v-if="submitted && !person.cn?.trim()" class="text-red-500"
                  >Name is required.</small
                >
              </UFormField>
              <UFormField label="Surname">
                <UInput v-model="person.sn" :disabled="!person.editable" class="w-full" />
                <small class="text-muted-color">OpenLDAP requires a surname (sn).</small>
                <small v-if="submitted && !person.sn?.trim()" class="text-red-500"
                  >Surname is required.</small
                >
              </UFormField>
              <UFormField label="Email">
                <UInput v-model="person.mail" :disabled="!person.editable" class="w-full" />
              </UFormField>
              <UFormField label="Display name">
                <UInput v-model="person.display_name" :disabled="!person.editable" class="w-full" />
              </UFormField>
              <UFormField label="Mobile">
                <UInput v-model="person.mobile" :disabled="!person.editable" class="w-full" />
              </UFormField>
              <UFormField v-if="person.editable" label="Password">
                <PasswordInput
                  v-model="person.password"
                  :color="submitted && !person.dn && !person.password ? 'error' : undefined"
                  :highlight="submitted && !person.dn && !person.password"
                />
                <small v-if="submitted && !person.dn && !person.password" class="text-red-500"
                  >Password is required.</small
                >
                <small v-else-if="person.dn" class="text-muted-color"
                  >Leave blank to keep the current password.</small
                >
              </UFormField>
              <div class="flex gap-2">
                <UButton
                  v-if="person.dn && person.editable"
                  label="Delete"
                  icon="i-lucide-trash"
                  color="error"
                  variant="ghost"
                  :loading="personDeleting"
                  @click="performDeletePerson()"
                />
              </div>
            </form>

            <form
              v-else-if="panel === 'group'"
              id="directory-group-form"
              class="flex flex-col gap-4"
              @submit.prevent="saveGroup"
            >
              <h5 class="m-0">{{ group.dn ? 'Directory group' : 'New directory group' }}</h5>
              <p v-if="groupLoading" class="text-sm text-muted-color">Loading…</p>
              <UAlert
                v-if="group.dn && !group.editable"
                color="neutral"
                variant="subtle"
                title="This group is shown read-only. Factum edits groupOfNames and groupOfUniqueNames entries on an OpenLDAP-compatible server."
              />
              <UFormField label="Container">
                <UInput
                  :model-value="group.dn ? parentOfDN(group.dn) : group.parent_dn"
                  readonly
                  class="w-full"
                />
              </UFormField>
              <UFormField label="Name">
                <UInput
                  v-model.trim="group.cn"
                  :disabled="!group.editable"
                  :color="submitted && !group.cn?.trim() ? 'error' : undefined"
                  :highlight="submitted && !group.cn?.trim()"
                  autofocus
                  class="w-full"
                />
                <small v-if="submitted && !group.cn?.trim()" class="text-red-500"
                  >Name is required.</small
                >
              </UFormField>
              <UFormField label="Description">
                <UTextarea
                  v-model="group.description"
                  :disabled="!group.editable"
                  :rows="3"
                  class="w-full"
                />
              </UFormField>
              <UFormField label="Members">
                <div class="flex flex-col gap-2">
                  <div
                    v-for="member in group.members ?? []"
                    :key="member"
                    class="flex items-center gap-2"
                  >
                    <span class="min-w-0 flex-1 truncate font-mono text-sm">{{ member }}</span>
                    <UButton
                      v-if="group.editable"
                      icon="i-lucide-x"
                      color="neutral"
                      variant="ghost"
                      size="sm"
                      aria-label="Remove member"
                      @click="removeMember(member)"
                    />
                  </div>
                  <UButton
                    v-if="group.editable"
                    label="Add member"
                    icon="i-lucide-plus"
                    color="neutral"
                    size="sm"
                    class="self-start"
                    @click="memberBrowser = true"
                  />
                  <small v-if="submitted && !(group.members ?? []).length" class="text-red-500">
                    A group needs at least one member.
                  </small>
                </div>
              </UFormField>
              <div class="flex gap-2">
                <UButton
                  v-if="group.dn && group.editable"
                  label="Delete"
                  icon="i-lucide-trash"
                  color="error"
                  variant="ghost"
                  :loading="groupDeleting"
                  @click="performDeleteGroup()"
                />
              </div>
            </form>
          </div>
        </div>
      </div>
    </template>
    <template #authorization>
      <AuthorizationSettings />
    </template>
  </UTabs>

  <LdapTreeBrowser v-model:visible="memberBrowser" selectable="person" @select="addMember" />

  <div
    v-if="menu.open && menuItems.length"
    class="ipam-context-menu"
    :style="{ left: menu.x + 'px', top: menu.y + 'px' }"
    @click.stop
  >
    <button
      v-for="item in menuItems"
      :key="item.id"
      :class="{ danger: item.danger }"
      type="button"
      @click="runMenu(item.id)"
    >
      {{ item.label }}
    </button>
  </div>
</template>

<style scoped>
/* The page fills the viewport, so this host is the leftover height under
   Search. A fixed 70vh host was taller than that leftover space: the page
   scrolled, and Wunderbaum's sticky header plus overflow:unset on the row
   list painted rows over the Directory title and under the toolbar.
   Clip and isolate so a row cannot leave this box. */
.directory-tree-host {
  flex: 1 1 auto;
  min-height: 0;
  overflow: clip;
  isolation: isolate;
  position: relative;
  z-index: 0;
}

.directory-tree-host :deep(.ipam-tree) {
  height: 100%;
  max-height: 100%;
  min-height: 0;
  flex: none;
  overflow: auto;
  overscroll-behavior: contain;
}

:deep(.ipam-tree span.wb-node i.wb-expander:not(.wb-indent)) {
  position: relative;
}

:deep(.ipam-tree span.wb-node i.wb-expander:not(.wb-indent)::after) {
  content: '';
  position: absolute;
  inset: -5px;
}
</style>
