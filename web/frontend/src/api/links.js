import http from './http'

export function getLinks() {
  return http.get('/links').then((res) => res.data)
}

export function getAdminLinks() {
  return http.get('/admin/links').then((res) => res.data)
}

export function createLink(payload) {
  return http.post('/admin/links', payload).then((res) => res.data)
}

export function updateLink(id, payload) {
  return http.put(`/admin/links/${id}`, payload).then((res) => res.data)
}

export function deleteLink(id) {
  return http.delete(`/admin/links/${id}`).then((res) => res.data)
}

export function reorderLinks(ids) {
  return http.put('/admin/links/reorder', { ids }).then((res) => res.data)
}
