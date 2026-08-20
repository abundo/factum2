import http from './http'

export function getLdapRoleMappings() {
  return http.get('/admin/ldap-role-mappings').then((res) => res.data)
}

export function createLdapRoleMapping(payload) {
  return http.post('/admin/ldap-role-mappings', payload).then((res) => res.data)
}

export function updateLdapRoleMapping(id, payload) {
  return http.put(`/admin/ldap-role-mappings/${id}`, payload).then((res) => res.data)
}

export function deleteLdapRoleMapping(id) {
  return http.delete(`/admin/ldap-role-mappings/${id}`).then((res) => res.data)
}
