import http from './http'

export function getSites() {
  return http.get('/sites').then((res) => res.data)
}

export function getSiteTree() {
  return http.get('/sites/tree').then((res) => res.data)
}

export function getSite(id) {
  return http.get(`/sites/${id}`).then((res) => res.data)
}

export function createSite(payload) {
  return http.post('/sites', payload).then((res) => res.data)
}

export function updateSite(id, payload) {
  return http.put(`/sites/${id}`, payload).then((res) => res.data)
}

export function deleteSite(id) {
  return http.delete(`/sites/${id}`).then((res) => res.data)
}
