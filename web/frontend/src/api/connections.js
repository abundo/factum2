import http from './http'

export function getConnections() {
  return http.get('/connections').then((res) => res.data)
}

export function getConnectionGraph(params = {}) {
  return http.get('/dcim/connections/graph', { params }).then((res) => res.data)
}

export function getConnectionLayout(scope) {
  return http
    .get(`/dcim/connection-view-layouts/${encodeURIComponent(scope)}`)
    .then((res) => res.data)
}

export function saveConnectionLayout(scope, payload) {
  return http
    .put(`/dcim/connection-view-layouts/${encodeURIComponent(scope)}`, payload)
    .then((res) => res.data)
}

export function getConnectionPair(a, b) {
  return http.get('/dcim/connections/pair', { params: { a, b } }).then((res) => res.data)
}

export function createConnection(payload) {
  return http.post('/dcim/connections', payload).then((res) => res.data)
}

export function updateConnection(id, payload) {
  return http.put(`/dcim/connections/${id}`, payload).then((res) => res.data)
}

export function deleteConnection(id) {
  return http.delete(`/dcim/connections/${id}`).then((res) => res.data)
}
