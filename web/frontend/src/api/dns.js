import http from './http'

function list(path) {
  return http.get(path).then((res) => res.data)
}
function get(path) {
  return http.get(path).then((res) => res.data)
}
function create(path, payload) {
  return http.post(path, payload).then((res) => res.data)
}
function update(path, payload) {
  return http.put(path, payload).then((res) => res.data)
}
function remove(path) {
  return http.delete(path)
}

export const listSOATemplates = () => list('/dns/soa-templates')
export const getSOATemplate = (id) => get(`/dns/soa-templates/${id}`)
export const createSOATemplate = (payload) => create('/dns/soa-templates', payload)
export const updateSOATemplate = (id, payload) => update(`/dns/soa-templates/${id}`, payload)
export const deleteSOATemplate = (id) => remove(`/dns/soa-templates/${id}`)

export const listDNSSECPolicies = () => list('/dns/dnssec-policies')
export const getDNSSECPolicy = (id) => get(`/dns/dnssec-policies/${id}`)
export const createDNSSECPolicy = (payload) => create('/dns/dnssec-policies', payload)
export const updateDNSSECPolicy = (id, payload) => update(`/dns/dnssec-policies/${id}`, payload)
export const deleteDNSSECPolicy = (id) => remove(`/dns/dnssec-policies/${id}`)

export const listDnsTemplates = () => list('/dns/templates')
export const getDnsTemplate = (id) => get(`/dns/templates/${id}`)
export const createDnsTemplate = (payload) => create('/dns/templates', payload)
export const updateDnsTemplate = (id, payload) => update(`/dns/templates/${id}`, payload)
export const deleteDnsTemplate = (id) => remove(`/dns/templates/${id}`)

export const listDhcpLeases = () =>
  list('/dns/leases').then((data) => (Array.isArray(data) ? data : (data?.leases ?? [])))

export const listDnsZones = () => list('/dns/zones')
export const getDnsZone = (id) => get(`/dns/zones/${id}`)
export const createDnsZone = (payload) => create('/dns/zones', payload)
export const updateDnsZone = (id, payload) => update(`/dns/zones/${id}`, payload)
export const deleteDnsZone = (id) => remove(`/dns/zones/${id}`)
