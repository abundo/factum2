import http from './http'

export function getUsers() {
  return http.get('/admin/users').then((res) => res.data)
}

export function getUser(id) {
  return http.get(`/admin/users/${id}`).then((res) => res.data)
}

export function createUser(payload) {
  return http.post('/admin/users', payload).then((res) => res.data)
}

export function updateUser(id, payload) {
  return http.put(`/admin/users/${id}`, payload).then((res) => res.data)
}

export function deleteUser(id) {
  return http.delete(`/admin/users/${id}`).then((res) => res.data)
}
