<script setup>
import { useToast } from '@nuxt/ui/composables'
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { Wunderbaum } from 'wunderbaum'
import {
  browseLdap,
  createLdapGroup,
  createLdapPerson,
  deleteLdapGroup,
  deleteLdapPerson,
  getLdapDirectory,
  getLdapEntry,
  updateLdapGroup,
  updateLdapPerson,
} from '@/api/ldap'
import { createRole, getRoles, updateRole } from '@/api/roles'
import { createUser, deleteUser, getUsers, updateUser } from '@/api/users'
import LdapTreeBrowser from '@/components/LdapTreeBrowser.vue'
import PasswordInput from '@/components/PasswordInput.vue'
import SearchInput from '@/components/SearchInput.vue'
import { useConfirm } from '@/composables/useConfirm'
import { confirmDiscard, usePageForm } from '@/composables/useFormGuard'
import { ldapKindLabel, toLdapNode } from '@/utils/ldapTree'
import { searchWords } from '@/utils/search'
import 'wunderbaum/dist/wunderbaum.css'
import '@/assets/wunderbaum-theme.css'

defineOptions({ name: 'DirectoryPage' })

const toast = useToast()
const { confirmDelete, ask } = useConfirm()

const el = ref(null)
const users = ref([])
const roles = ref([])
const directory = ref({ enabled: false, server_type: 'generic', base_dn: '', writable: false })
const loading = ref(true)
const error = ref(null)
const forbidden = ref(false)
const selected = ref(null)
const search = ref('')
const panel = ref('')
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
  return null
})
const { mark, dirty: formDirty } = usePageForm(activeForm)

const writable = computed(() => !!directory.value.writable)
const ldapOn = computed(() => !!directory.value.enabled)
const ad = computed(() => directory.value.server_type === 'ad')

const intro = computed(() => {
  if (!ldapOn.value) return 'Local users and roles.'
  if (ad.value) {
    return 'Active Directory is shown for browsing. Accounts are managed in Active Directory. Local users and roles are under Local.'
  }
  if (!writable.value) {
    return 'OpenLDAP is shown read-only. Set ldap_writeback.bind_dn and ldap_writeback.bind_password in the config file to add and change entries.'
  }
  return 'Local accounts are under Local. OpenLDAP users and groups can be added under a directory container.'
})

const menuItems = computed(() => {
  if (!menu.value.open) return []
  const node = menu.value.node
  const kind = node?.kind || ''
  const items = []
  if (!node || kind === 'local' || kind === 'users') {
    items.push({ id: 'new-user', label: 'New user' })
  }
  if (!node || kind === 'local' || kind === 'roles') {
    items.push({ id: 'new-role', label: 'New role' })
  }
  const ldapHere = kind === 'directory' || kind === 'container' || kind === 'other'
  if (writable.value && (!node || ldapHere)) {
    items.push({ id: 'new-person', label: 'New directory user' })
    items.push({ id: 'new-group', label: 'New group' })
  }
  if (kind === 'user' || kind === 'role' || kind === 'person' || kind === 'group') {
    items.push({ id: 'edit', label: 'Edit' })
  }
  if (kind === 'user' && !isAdmin(node)) items.push({ id: 'delete', label: 'Delete', danger: true })
  if ((kind === 'person' || kind === 'group') && writable.value) {
    items.push({ id: 'delete', label: 'Delete', danger: true })
  }
  return items
})

const selectedKind = computed(() => selected.value?.kind || '')
const ldapSelection = computed(() =>
  ['directory', 'container', 'other', 'person', 'group'].includes(selectedKind.value),
)

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

function userNodes() {
  return users.value.map((u) => ({
    key: `local-user-${u.id}`,
    title: u.username,
    type: 'user',
    kind: 'user',
    user_id: u.id,
    username: u.username,
    name: u.name || '',
    email: u.email || '',
    mobile: u.mobile || '',
    role_ids: u.role_ids ?? [],
  }))
}

function roleNodes() {
  return roles.value.map((r) => ({
    key: `local-role-${r.id}`,
    title: r.name,
    type: 'role',
    kind: 'role',
    role_id: r.id,
    name: r.name,
    description: r.description || '',
  }))
}

function localBranch() {
  return {
    key: 'local',
    title: 'Local',
    type: 'local',
    kind: 'local',
    expanded: true,
    children: [
      {
        key: 'local-users',
        title: 'Users',
        type: 'users',
        kind: 'users',
        expanded: true,
        children: userNodes(),
      },
      {
        key: 'local-roles',
        title: 'Roles',
        type: 'roles',
        kind: 'roles',
        expanded: true,
        children: roleNodes(),
      },
    ],
  }
}

function directoryBranch() {
  if (!ldapOn.value) return null
  const base = directory.value.base_dn || ''
  return {
    key: base || 'directory',
    title: base || 'Directory',
    lazy: true,
    type: 'directory',
    kind: 'directory',
    dn: base,
  }
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
  const kind = node.data?.kind || node.type
  return !['user', 'role', 'person', 'group'].includes(kind)
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
  const source = [localBranch()]
  const dir = directoryBranch()
  if (dir) source.push(dir)
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
      local: { icon: false },
      users: { icon: false },
      roles: { icon: false },
      user: { icon: false },
      role: { icon: false },
      directory: { icon: false },
      container: { icon: false },
      person: { icon: false },
      group: { icon: false },
      other: { icon: false },
    },
    lazyLoad: (e) => {
      const dn = e.node.data?.dn ?? ''
      return browseLdap(dn).then((rows) => (rows ?? []).map(toLdapNode))
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
  })
  bindContextMenu()
  bindExpander()
  applySearch()
}

function syncLocal() {
  if (!tree) {
    buildTree()
    return
  }
  const usersNode = tree.findKey('local-users')
  const rolesNode = tree.findKey('local-roles')
  if (!usersNode || !rolesNode) {
    buildTree()
    return
  }
  // removeChildren collapses the folder. Put the rows back and reopen it
  // when it was open, so a save does not hide the user that was just added.
  const usersOpen = usersNode.expanded !== false
  const rolesOpen = rolesNode.expanded !== false
  usersNode.removeChildren()
  usersNode.addChildren(userNodes())
  rolesNode.removeChildren()
  rolesNode.addChildren(roleNodes())
  if (usersOpen) usersNode.setExpanded(true)
  if (rolesOpen) rolesNode.setExpanded(true)
  applySearch()
}

async function refreshLdap(parentDN) {
  if (!tree || !parentDN) return
  const node = tree.findKey(parentDN)
  if (!node) return
  node.resetLazy()
  await node.setExpanded(true)
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
  const base = directory.value.base_dn || ''
  if (!node) return base
  if (node.kind === 'directory' || node.kind === 'container' || node.kind === 'other') {
    return node.dn || node.key || base
  }
  if (node.kind === 'person' || node.kind === 'group') {
    return parentOfDN(node.dn || node.key || '') || base
  }
  return base
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
      password: '',
    }
  } else if (n.kind === 'role') {
    panel.value = 'role'
    role.value = { id: n.role_id, name: n.name, description: n.description || '' }
  } else if (n.kind === 'person') {
    panel.value = 'person'
    personLoading.value = true
    person.value = { dn: n.dn || n.key, editable: false, password: '' }
    try {
      const entry = await getLdapEntry(n.dn || n.key)
      if (seq !== fillSeq) return
      person.value = { ...entry, password: '' }
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
      const entry = await getLdapEntry(n.dn || n.key)
      if (seq !== fillSeq) return
      group.value = { ...entry, members: entry.members ?? [] }
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
  user.value = { role_ids: [] }
  await nextTick()
  mark()
}

async function openNewRole() {
  if (!(await guardEdits())) return
  creating.value = true
  submitted.value = false
  panel.value = 'role'
  role.value = {}
  await nextTick()
  mark()
}

async function openNewPerson(parent) {
  if (!(await guardEdits())) return
  creating.value = true
  submitted.value = false
  panel.value = 'person'
  person.value = {
    parent_dn: parent,
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
  const parent = node ? ldapParentDN(node) : ldapParentDN(selected.value)
  creating.value = true
  submitted.value = false
  panel.value = 'group'
  group.value = { parent_dn: parent, cn: '', description: '', members: [], editable: true }
  await nextTick()
  mark()
}

function openNewUser() {
  if (writable.value && ldapSelection.value) {
    return openNewPerson(ldapParentDN(selected.value))
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
      toastOk(body.id ? 'User updated' : 'User created')
      creating.value = false
      mark()
      await reloadLocal()
      await activateKey(`local-user-${saved?.id || body.id}`)
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
      toastOk(body.id ? 'Role updated' : 'Role created')
      creating.value = false
      mark()
      await reloadLocal()
      await activateKey(`local-role-${saved?.id || body.id}`)
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
      await refreshLdap(parent)
      await activateKey(saved?.dn || body.dn)
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
  deleteLdapPerson(row.dn)
    .then(async (res) => {
      mark()
      if (person.value.dn === row.dn) panel.value = ''
      await refreshLdap(parent)
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
      await refreshLdap(parent)
      await activateKey(saved?.dn || body.dn)
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
  deleteLdapGroup(row.dn)
    .then(async () => {
      toastOk('Directory group deleted')
      mark()
      if (group.value.dn === row.dn) panel.value = ''
      await refreshLdap(parent)
    })
    .catch((err) => toastError(err, 'Failed to delete the directory group.'))
    .finally(() => {
      groupDeleting.value = false
    })
}

function deleteNode(node) {
  if (!node) return
  if (node.kind === 'user') return performDeleteUser(node)
  if (node.kind === 'person') return performDeletePerson(node)
  if (node.kind === 'group') return performDeleteGroup(node)
}

function runMenu(id) {
  const node = menu.value.node
  closeMenu()
  if (id === 'new-user') return openLocalUser()
  if (id === 'new-role') return openNewRole()
  if (id === 'new-person') return openNewPerson(ldapParentDN(node))
  if (id === 'new-group') return openNewGroup(node)
  if (id === 'edit') return showSelection(node)
  if (id === 'delete') return deleteNode(node)
}

function onDocClick() {
  if (menu.value.open) closeMenu()
}

function reloadLocal() {
  return Promise.all([getUsers(), getRoles()]).then(([userRows, roleRows]) => {
    users.value = userRows ?? []
    roles.value = roleRows ?? []
    syncLocal()
  })
}

function load() {
  loading.value = true
  error.value = null
  forbidden.value = false
  Promise.all([getUsers(), getRoles(), getLdapDirectory()])
    .then(([userRows, roleRows, status]) => {
      users.value = userRows ?? []
      roles.value = roleRows ?? []
      directory.value = status ?? directory.value
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
  <div v-else class="card">
    <div class="mb-4 flex flex-wrap items-center justify-between gap-3">
      <div>
        <h4 class="m-0">Directory</h4>
        <small class="text-muted-color">{{ intro }}</small>
      </div>
      <div class="flex flex-wrap gap-2">
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
          v-if="writable"
          label="New group"
          icon="i-lucide-plus"
          color="neutral"
          size="sm"
          @click="openNewGroup()"
        />
      </div>
    </div>
    <UAlert v-if="error" color="error" variant="subtle" :title="error" class="mb-3" />
    <div class="flex flex-col gap-4 lg:flex-row lg:items-start">
      <div class="w-full shrink-0 lg:w-96">
        <SearchInput v-model="search" class="mb-3" />
        <div class="directory-tree-host">
          <div ref="el" class="ipam-tree" />
        </div>
        <p v-if="loading" class="mt-2 text-sm text-muted-color">Loading…</p>
      </div>
      <div class="min-w-0 flex-1 lg:max-h-[min(70vh,36rem)] lg:overflow-auto">
        <p v-if="!panel" class="text-sm text-muted-color">
          Select an entry. Click + or − to open a folder. Right-click a row to add, change, or
          remove it.
        </p>
        <div v-else-if="panel === 'node'" class="flex flex-col gap-2">
          <h5 class="m-0">{{ selected?.title }}</h5>
          <p class="text-sm text-muted-color">{{ ldapKindLabel(selected?.kind) }}</p>
          <p v-if="selected?.dn" class="font-mono text-sm break-all">{{ selected.dn }}</p>
        </div>
        <form v-else-if="panel === 'user'" class="flex flex-col gap-4" @submit.prevent="saveUser">
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
            <UButton
              class="ms-auto"
              label="Save"
              icon="i-lucide-check"
              type="submit"
              :loading="userSaving"
            />
          </div>
        </form>

        <form v-else-if="panel === 'role'" class="flex flex-col gap-4" @submit.prevent="saveRole">
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
          <div class="flex gap-2">
            <UButton
              class="ms-auto"
              label="Save"
              icon="i-lucide-check"
              type="submit"
              :loading="roleSaving"
            />
          </div>
        </form>

        <form
          v-else-if="panel === 'person'"
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
            <UButton
              v-if="person.editable"
              class="ms-auto"
              label="Save"
              icon="i-lucide-check"
              type="submit"
              :loading="personSaving"
            />
          </div>
        </form>

        <form v-else-if="panel === 'group'" class="flex flex-col gap-4" @submit.prevent="saveGroup">
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
            <UButton
              v-if="group.editable"
              class="ms-auto"
              label="Save"
              icon="i-lucide-check"
              type="submit"
              :loading="groupSaving"
            />
          </div>
        </form>
      </div>
    </div>
  </div>

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
/* .ipam-tree is height:100%. This host gives that percentage a real box, so
   the row list scrolls instead of growing with every loaded entry. */
.directory-tree-host {
  height: min(70vh, 36rem);
  min-height: 16rem;
}

.directory-tree-host :deep(.ipam-tree) {
  height: 100%;
  flex: none;
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
