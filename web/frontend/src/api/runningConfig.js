import http from './http'

export function getRunningConfigPlatforms() {
  return http.get('/dcim/running-config/platforms').then((res) => res.data.platforms ?? [])
}

export function getRunningConfig(deviceId) {
  return http.get(`/dcim/running-config/${deviceId}`).then((res) => res.data)
}

export function commitRunningConfig(deviceId, payload) {
  return http.post(`/dcim/running-config/${deviceId}`, payload).then((res) => res.data)
}
