import http from './http'

export function getTopology() {
  return http.get('/topology').then((res) => res.data)
}
