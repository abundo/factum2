import http from './http'

export function testLdapConnection(payload) {
  return http.post('/admin/ldap/test', payload).then((res) => res.data)
}

// browseLdap lists the immediate children of dn (or the configured base DN
// if dn is omitted) for the LDAP tree browser.
export function browseLdap(dn) {
  return http.get('/admin/ldap/browse', { params: dn ? { dn } : {} }).then((res) => res.data)
}

export function getLdapDirectory() {
  return http.get('/admin/ldap/directory').then((res) => res.data)
}

export function getLdapEntry(dn) {
  return http.get('/admin/ldap/entry', { params: { dn } }).then((res) => res.data)
}

export function createLdapPerson(payload) {
  return http.post('/admin/ldap/people', payload).then((res) => res.data)
}

export function updateLdapPerson(payload) {
  return http.put('/admin/ldap/people', payload).then((res) => res.data)
}

export function deleteLdapPerson(dn) {
  return http.delete('/admin/ldap/people', { params: { dn } }).then((res) => res.data)
}

export function createLdapGroup(payload) {
  return http.post('/admin/ldap/groups', payload).then((res) => res.data)
}

export function updateLdapGroup(payload) {
  return http.put('/admin/ldap/groups', payload).then((res) => res.data)
}

export function deleteLdapGroup(dn) {
  return http.delete('/admin/ldap/groups', { params: { dn } }).then((res) => res.data)
}
