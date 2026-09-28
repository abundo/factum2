import http from './http'

export function getRadiusClients() {
  return http.get('/admin/radius/clients').then((res) => res.data)
}

export function getRadiusClientSecret(id) {
  return http.get(`/admin/radius/clients/${id}/secret`).then((res) => res.data)
}

export function createRadiusClient(payload) {
  return http.post('/admin/radius/clients', payload).then((res) => res.data)
}

export function updateRadiusClient(id, payload) {
  return http.put(`/admin/radius/clients/${id}`, payload).then((res) => res.data)
}

export function deleteRadiusClient(id) {
  return http.delete(`/admin/radius/clients/${id}`).then((res) => res.data)
}

export function getRadiusPolicies() {
  return http.get('/admin/radius/policies').then((res) => res.data)
}

export function createRadiusPolicy(payload) {
  return http.post('/admin/radius/policies', payload).then((res) => res.data)
}

export function updateRadiusPolicy(id, payload) {
  return http.put(`/admin/radius/policies/${id}`, payload).then((res) => res.data)
}

export function deleteRadiusPolicy(id) {
  return http.delete(`/admin/radius/policies/${id}`).then((res) => res.data)
}

export function getRadiusDeviceRoles() {
  return http.get('/admin/radius/device-roles').then((res) => res.data)
}

export function getRadiusEvents() {
  return http.get('/admin/radius/events').then((res) => res.data)
}
