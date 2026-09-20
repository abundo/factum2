import http from './http'

export function getBranding() {
  return http.get('/branding').then((res) => res.data)
}
