import http from './http'

export function getTopology() {
  return http.get('/topology').then((res) => res.data)
}

export function getTopologyDevices() {
  return http.get('/topology/devices').then((res) => res.data)
}

export function assignDeviceLocation(id, body) {
  return http.post(`/topology/devices/${id}/location`, body).then((res) => res.data)
}

export function reverseGeocode(lat, lng) {
  return http.get('/topology/geocode', { params: { lat, lng } }).then((res) => res.data)
}
