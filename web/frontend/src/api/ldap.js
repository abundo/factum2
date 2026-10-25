import http from './http'

export function testLdapConnection(payload) {
  return http.post('/admin/ldap/test', payload).then((res) => res.data)
}

export function getLdapServers() {
  return http.get('/admin/ldap/servers').then((res) => res.data)
}

export function createLdapServer(payload) {
  return http.post('/admin/ldap/servers', payload).then((res) => res.data)
}

export function updateLdapServer(id, payload) {
  return http.put(`/admin/ldap/servers/${id}`, payload).then((res) => res.data)
}

export function deleteLdapServer(id) {
  return http.delete(`/admin/ldap/servers/${id}`).then((res) => res.data)
}

// browseLdap lists the immediate children of dn on one external server.
// serverId is omitted for the group picker, which uses the first enabled server.
export function browseLdap(dn, serverId) {
  const params = {}
  if (dn) params.dn = dn
  if (serverId) params.server_id = serverId
  return http.get('/admin/ldap/browse', { params }).then((res) => res.data)
}

export function getLdapDirectory() {
  return http.get('/admin/ldap/directory').then((res) => res.data)
}

export function getLdapEntry(dn, serverId) {
  const params = { dn }
  if (serverId) params.server_id = serverId
  return http.get('/admin/ldap/entry', { params }).then((res) => res.data)
}

export function createLdapPerson(payload) {
  return http.post('/admin/ldap/people', payload).then((res) => res.data)
}

export function updateLdapPerson(payload) {
  return http.put('/admin/ldap/people', payload).then((res) => res.data)
}

export function deleteLdapPerson(dn, serverId) {
  const params = { dn }
  if (serverId) params.server_id = serverId
  return http.delete('/admin/ldap/people', { params }).then((res) => res.data)
}

export function createLdapGroup(payload) {
  return http.post('/admin/ldap/groups', payload).then((res) => res.data)
}

export function updateLdapGroup(payload) {
  return http.put('/admin/ldap/groups', payload).then((res) => res.data)
}

export function deleteLdapGroup(dn, serverId) {
  const params = { dn }
  if (serverId) params.server_id = serverId
  return http.delete('/admin/ldap/groups', { params }).then((res) => res.data)
}
