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
