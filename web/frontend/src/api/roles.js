import http from './http'

export function getRoles() {
  return http.get('/admin/roles').then((res) => res.data)
}

export function getRole(id) {
  return http.get(`/admin/roles/${id}`).then((res) => res.data)
}

export function createRole(payload) {
  return http.post('/admin/roles', payload).then((res) => res.data)
}

export function updateRole(id, payload) {
  return http.put(`/admin/roles/${id}`, payload).then((res) => res.data)
}
