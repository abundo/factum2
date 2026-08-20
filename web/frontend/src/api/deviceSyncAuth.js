import http from './http'

export function getDeviceSyncAuths() {
  return http.get('/admin/device-sync-auth').then((res) => res.data)
}

export function getDeviceSyncAuth(id) {
  return http.get(`/admin/device-sync-auth/${id}`).then((res) => res.data)
}

export function createDeviceSyncAuth(payload) {
  return http.post('/admin/device-sync-auth', payload).then((res) => res.data)
}

export function updateDeviceSyncAuth(id, payload) {
  return http.put(`/admin/device-sync-auth/${id}`, payload).then((res) => res.data)
}

export function deleteDeviceSyncAuth(id) {
  return http.delete(`/admin/device-sync-auth/${id}`).then((res) => res.data)
}

export function getDeviceSyncAuthPassword(id) {
  return http.get(`/admin/device-sync-auth/${id}/password`).then((res) => res.data)
}
