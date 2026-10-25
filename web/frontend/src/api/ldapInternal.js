import http from './http'

export function getLdapInternal() {
  return http.get('/admin/ldap/internal').then((res) => res.data)
}

export function updateLdapInternal(payload) {
  return http.put('/admin/ldap/internal', payload).then((res) => res.data)
}

export function createLdapOU(payload) {
  return http.post('/admin/ldap/ous', payload).then((res) => res.data)
}

export function updateLdapOU(id, payload) {
  return http.put(`/admin/ldap/ous/${id}`, payload).then((res) => res.data)
}

export function deleteLdapOU(id) {
  return http.delete(`/admin/ldap/ous/${id}`)
}

export function placeLdap(payload) {
  return http.put('/admin/ldap/place', payload)
}

export function setRoleMembers(id, memberRoleIDs) {
  return http.put(`/admin/ldap/roles/${id}/members`, { member_role_ids: memberRoleIDs })
}
