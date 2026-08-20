import http from './http'

export function testLdapConnection(payload) {
  return http.post('/admin/ldap/test', payload).then((res) => res.data)
}

// browseLdap lists the immediate children of dn (or the configured base DN
// if dn is omitted) for the LDAP tree browser.
export function browseLdap(dn) {
  return http.get('/admin/ldap/browse', { params: dn ? { dn } : {} }).then((res) => res.data)
}
