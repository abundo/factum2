import http from './http'

export function getWorkerNodes() {
  return http.get('/admin/worker-nodes').then((res) => res.data)
}

export function getWorkerNode(id) {
  return http.get(`/admin/worker-nodes/${id}`).then((res) => res.data)
}

export function createWorkerNode(payload) {
  return http.post('/admin/worker-nodes', payload).then((res) => res.data)
}

export function updateWorkerNode(id, payload) {
  return http.put(`/admin/worker-nodes/${id}`, payload).then((res) => res.data)
}

export function getWorkerNodeToken(id) {
  return http.get(`/admin/worker-nodes/${id}/token`).then((res) => res.data)
}
