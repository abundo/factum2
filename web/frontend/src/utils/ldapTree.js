// Shared LDAP tree nodes for the group picker and the directory page.
// Extra fields (kind, dn, is_group) land on Wunderbaum's node.data.

export function ldapKind(entry) {
  if (entry?.kind) return entry.kind
  if (entry?.is_group) return 'group'
  return 'container'
}

export function toLdapNode(entry) {
  const kind = ldapKind(entry)
  const leaf = kind === 'group' || kind === 'person'
  return {
    key: entry.dn,
    title: entry.name || entry.dn,
    lazy: !leaf,
    type: kind,
    kind,
    dn: entry.dn,
    is_group: kind === 'group',
  }
}

// Folders stay above people and roles. Each group is alphabetical, ignoring case.
export function compareDirectoryNodes(a, b) {
  const aFolder = (a?.kind || a?.type) === 'ou'
  const bFolder = (b?.kind || b?.type) === 'ou'
  if (aFolder !== bFolder) return aFolder ? -1 : 1
  const aTitle = a?.title || a?.name || ''
  const bTitle = b?.title || b?.name || ''
  return aTitle.localeCompare(bTitle, undefined, { sensitivity: 'base' })
}

export function sortDirectoryNodes(nodes) {
  return (nodes ?? []).slice().sort(compareDirectoryNodes)
}

// internalDropRegions is where a user, role, or folder may land.
// overId is the folder under the pointer (0 is the internal root).
// aroundParent is that row's parent folder, or null when the row is not
// inside the internal directory. intoSelf is true when the destination is
// the dragged folder or a folder inside it. A drop in the same folder is
// refused: siblings stay in name order.
export function internalDropRegions({ sourceParent, targetKind, overId, aroundParent, intoSelf }) {
  const regions = []
  if ((targetKind === 'ou' || targetKind === 'internal') && overId !== sourceParent && !intoSelf) {
    regions.push('over')
  }
  if (
    aroundParent != null &&
    aroundParent !== sourceParent &&
    !intoSelf &&
    (targetKind === 'user' || targetKind === 'role' || targetKind === 'ou' || targetKind === 'internal')
  ) {
    regions.push('before', 'after')
  }
  return regions.length ? regions : false
}

// serverDropRegions reorders external servers among themselves. The local
// directory is not a target, so a server cannot be placed ahead of it.
export function serverDropRegions(targetKind) {
  if (targetKind === 'server') return ['before', 'after']
  return false
}

// serverOrderAfterDrop returns server ids with sourceId inserted beside
// targetId. mode is before or after. A target that is not a server is refused.
export function serverOrderAfterDrop(serverIds, sourceId, targetKind, targetId, mode) {
  if (targetKind !== 'server') return null
  if (mode !== 'before' && mode !== 'after') return null
  const ids = (serverIds ?? []).filter((id) => id !== sourceId)
  let index = ids.indexOf(targetId)
  if (index < 0) return null
  if (mode === 'after') index += 1
  ids.splice(index, 0, sourceId)
  return ids
}

export function ldapKindLabel(kind) {
  switch (kind) {
    case 'local':
      return 'Local'
    case 'users':
      return 'Users'
    case 'roles':
      return 'Roles'
    case 'user':
      return 'User'
    case 'role':
      return 'Role'
    case 'person':
      return 'Person'
    case 'group':
      return 'Group'
    case 'container':
      return 'Container'
    case 'directory':
      return 'Directory'
    case 'internal':
      return 'Internal'
    case 'server':
      return 'Server'
    case 'ou':
      return 'Folder'
    case 'other':
      return 'Entry'
    default:
      return kind || ''
  }
}
