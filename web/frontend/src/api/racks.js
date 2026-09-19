import http from './http'

export function getRacks(params = {}) {
  return http.get('/dcim/racks', { params }).then((res) => res.data)
}

export function createRack(payload) {
  return http.post('/dcim/racks', payload).then((res) => res.data)
}

export function updateRack(id, payload) {
  return http.put(`/dcim/racks/${id}`, payload).then((res) => res.data)
}

export function deleteRack(id) {
  return http.delete(`/dcim/racks/${id}`).then((res) => res.data)
}

export function getRackElevation(id) {
  return http.get(`/dcim/racks/${id}/elevation`).then((res) => res.data)
}

export function placeDevice(deviceId, payload) {
  return http.put(`/dcim/devices/${deviceId}/placement`, payload).then((res) => res.data)
}

export function unmountDevice(deviceId, version) {
  const params = version ? { version } : {}
  return http.delete(`/dcim/devices/${deviceId}/placement`, { params }).then((res) => res.data)
}
