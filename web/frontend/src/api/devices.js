import http from './http'

export function getDevices() {
  return http.get('/device').then((res) => res.data)
}

export function createDevice(payload) {
  return http.post('/device', payload).then((res) => res.data)
}

export function updateDevice(id, payload) {
  return http.put(`/device/${id}`, payload).then((res) => res.data)
}

export function deleteDevice(id) {
  return http.delete(`/device/${id}`).then((res) => res.data)
}

export function getDevice(id) {
  return http.get(`/device/${id}`).then((res) => res.data)
}

export function getDeviceImpact(id) {
  return http.get(`/device/${id}/impact`).then((res) => res.data)
}

// Fetch live interfaces from the device, overwrite stored descriptions,
// and drop factum/Netbox interfaces that no longer exist on the device
// (device-type template ports are kept). Device login uses Admin → Device
// sync credentials on the server.
export function refreshDeviceInterfaces(id) {
  return http.post(`/device/${id}/interfaces/refresh`).then((res) => res.data)
}

// Push edited interface descriptions out to the device, Netbox, and
// factum's own interface table. Device login uses Admin → Device sync
// credentials on the server.
export function updateDeviceInterfaces(id, interfaces) {
  return http.post(`/device/${id}/interfaces/update`, { interfaces }).then((res) => res.data)
}

// Push edited switchport/VLAN config out to the device, Netbox, and
// factum's own interface table. Each entry in `interfaces` is
// { id, switchport_mode, untagged_vlan, tagged_vlans }. Device login uses
// Admin → Device sync credentials on the server.
export function updateInterfaceVlans(id, interfaces) {
  return http.post(`/device/${id}/interfaces/vlans`, { interfaces }).then((res) => res.data)
}
