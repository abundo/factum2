import http from './http'

export function listKindMaps() {
  return http.get('/optical/kind-maps').then((res) => res.data)
}
export function createKindMap(payload) {
  return http.post('/optical/kind-maps', payload).then((res) => res.data)
}
export function updateKindMap(id, payload) {
  return http.put(`/optical/kind-maps/${id}`, payload).then((res) => res.data)
}
export function deleteKindMap(id) {
  return http.delete(`/optical/kind-maps/${id}`)
}
export function putOpticalPort(interfaceId, payload) {
  return http.put(`/optical/ports/${interfaceId}`, payload).then((res) => res.data)
}
export function deleteOpticalPort(interfaceId) {
  return http.delete(`/optical/ports/${interfaceId}`)
}
export function listXConnects(deviceId) {
  return http.get('/optical/xconnects', { params: { device_id: deviceId } }).then((res) => res.data)
}
export function createXConnect(payload) {
  return http.post('/optical/xconnects', payload).then((res) => res.data)
}
export function deleteXConnect(id) {
  return http.delete(`/optical/xconnects/${id}`)
}
export function traceOptical(payload) {
  return http.post('/optical/trace', payload).then((res) => res.data)
}
export function getServicePath(serviceId) {
  return http.get(`/service/${serviceId}/path`).then((res) => res.data)
}
export function putServicePath(serviceId, payload) {
  return http.put(`/service/${serviceId}/path`, payload).then((res) => res.data)
}
