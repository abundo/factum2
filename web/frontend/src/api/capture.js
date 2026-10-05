import http from './http'

// Platforms whose driver can set up a port mirror, each with the interface
// name prefixes that platform can source. The packet-capture device picker
// shows only these.
export function getCapturePlatforms() {
  return http.get('/capture/platforms').then((res) => res.data.platforms ?? [])
}
