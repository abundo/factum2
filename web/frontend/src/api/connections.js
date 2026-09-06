import http from './http'

export function getConnections() {
  return http.get('/connections').then((res) => res.data)
}
