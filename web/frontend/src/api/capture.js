import http from './http'

// Platforms whose driver can set up a port mirror. The packet-capture
// device picker shows only these.
export function getCapturePlatforms() {
  return http.get('/capture/platforms').then((res) => res.data.platforms ?? [])
}
