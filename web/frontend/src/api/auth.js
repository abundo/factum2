import http from './http'

export function login(username, password) {
  return http.post('/login', { username, password }).then((res) => res.data)
}

export function logout() {
  return http.post('/logout').then((res) => res.data)
}

export function forgotPassword(email) {
  return http.post('/forgot-password', { email }).then((res) => res.data)
}

export function resetPassword(payload) {
  return http.post('/reset-password', payload).then((res) => res.data)
}
