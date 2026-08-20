import http from './http'

export function listNamespaces() {
  return http.get('/ipam/namespaces').then((res) => res.data)
}
export function getNamespace(id) {
  return http.get(`/ipam/namespaces/${id}`).then((res) => res.data)
}
export function createNamespace(payload) {
  return http.post('/ipam/namespaces', payload).then((res) => res.data)
}
export function updateNamespace(id, payload) {
  return http.put(`/ipam/namespaces/${id}`, payload).then((res) => res.data)
}
export function deleteNamespace(id) {
  return http.delete(`/ipam/namespaces/${id}`)
}

export function createPool(namespaceId, payload) {
  return http.post(`/ipam/namespaces/${namespaceId}/pools`, payload).then((res) => res.data)
}
export function deletePool(namespaceId, poolId) {
  return http.delete(`/ipam/namespaces/${namespaceId}/pools/${poolId}`)
}

export function createVrf(namespaceId, payload) {
  return http.post(`/ipam/namespaces/${namespaceId}/vrfs`, payload).then((res) => res.data)
}
export function updateVrf(namespaceId, vrfId, payload) {
  return http.put(`/ipam/namespaces/${namespaceId}/vrfs/${vrfId}`, payload).then((res) => res.data)
}
export function deleteVrf(namespaceId, vrfId) {
  return http.delete(`/ipam/namespaces/${namespaceId}/vrfs/${vrfId}`)
}

export function createPrefix(namespaceId, payload) {
  return http.post(`/ipam/namespaces/${namespaceId}/prefixes`, payload).then((res) => res.data)
}
export function updatePrefix(namespaceId, prefixId, payload) {
  return http
    .put(`/ipam/namespaces/${namespaceId}/prefixes/${prefixId}`, payload)
    .then((res) => res.data)
}
export function deletePrefix(namespaceId, prefixId) {
  return http.delete(`/ipam/namespaces/${namespaceId}/prefixes/${prefixId}`)
}

export function getTree(namespaceId, prefix) {
  return http
    .get(`/ipam/namespaces/${namespaceId}/tree`, { params: prefix ? { prefix } : {} })
    .then((res) => res.data ?? [])
}

export function getForest(parent) {
  return http.get('/ipam/tree', { params: parent ? { parent } : {} }).then((res) => res.data ?? [])
}
