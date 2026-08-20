import http from './http'

export function getMe() {
  return http.get('/me').then((res) => res.data)
}

export function updateMe(payload) {
  return http.put('/me', payload).then((res) => res.data)
}
