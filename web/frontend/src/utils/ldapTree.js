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
    case 'ou':
      return 'Folder'
    case 'other':
      return 'Entry'
    default:
      return kind || ''
  }
}
