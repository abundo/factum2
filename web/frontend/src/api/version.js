import http from './http'

export function getVersion() {
  return http.get('/version').then((res) => res.data)
}
