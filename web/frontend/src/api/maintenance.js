import http from './http'

export function listMaintenance() {
  return http.get('/maintenance').then((res) => res.data)
}
export function getMaintenance(id) {
  return http.get(`/maintenance/${id}`).then((res) => res.data)
}
export function createMaintenance(payload) {
  return http.post('/maintenance', payload).then((res) => res.data)
}
export function updateMaintenance(id, payload) {
  return http.put(`/maintenance/${id}`, payload).then((res) => res.data)
}
export function notifyMaintenance(id, force = false) {
  return http.post(`/maintenance/${id}/notify`, { force }).then((res) => res.data)
}
