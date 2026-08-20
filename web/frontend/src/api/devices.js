import http from './http'

export function getDevices() {
  return http.get('/device').then((res) => res.data)
}

export function getDevice(id) {
  return http.get(`/device/${id}`).then((res) => res.data)
}

export function getDeviceImpact(id) {
  return http.get(`/device/${id}/impact`).then((res) => res.data)
}

// Fetch interface descriptions directly from the device (EOS only) and
// overwrite the stored Netbox/factum descriptions with what it reports.
export function refreshDeviceInterfaces(id, username, password) {
  return http
    .post(`/device/${id}/interfaces/refresh`, { username, password })
    .then((res) => res.data)
}

// Push edited interface descriptions out to the device (EOS only), Netbox,
// and factum's own interface table.
export function updateDeviceInterfaces(id, username, password, interfaces) {
  return http
    .post(`/device/${id}/interfaces/update`, { username, password, interfaces })
    .then((res) => res.data)
}

// Push edited switchport/VLAN config out to the device (EOS/VRP only),
// Netbox, and factum's own interface table. Each entry in `interfaces` is
// { id, switchport_mode, untagged_vlan, tagged_vlans }.
export function updateInterfaceVlans(id, username, password, interfaces) {
  return http
    .post(`/device/${id}/interfaces/vlans`, { username, password, interfaces })
    .then((res) => res.data)
}
