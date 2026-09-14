import http from './http'

export function getManufacturers() {
  return http.get('/dcim/manufacturers').then((res) => res.data)
}

export function createManufacturer(payload) {
  return http.post('/dcim/manufacturers', payload).then((res) => res.data)
}

export function updateManufacturer(id, payload) {
  return http.put(`/dcim/manufacturers/${id}`, payload).then((res) => res.data)
}

export function deleteManufacturer(id) {
  return http.delete(`/dcim/manufacturers/${id}`).then((res) => res.data)
}

export function getDeviceTypes() {
  return http.get('/dcim/device-types').then((res) => res.data)
}

export function createDeviceType(payload) {
  return http.post('/dcim/device-types', payload).then((res) => res.data)
}

export function updateDeviceType(id, payload) {
  return http.put(`/dcim/device-types/${id}`, payload).then((res) => res.data)
}

export function deleteDeviceType(id) {
  return http.delete(`/dcim/device-types/${id}`).then((res) => res.data)
}

export function getPlatforms() {
  return http.get('/dcim/platforms').then((res) => res.data)
}

export function createPlatform(payload) {
  return http.post('/dcim/platforms', payload).then((res) => res.data)
}

export function updatePlatform(id, payload) {
  return http.put(`/dcim/platforms/${id}`, payload).then((res) => res.data)
}

export function deletePlatform(id) {
  return http.delete(`/dcim/platforms/${id}`).then((res) => res.data)
}
