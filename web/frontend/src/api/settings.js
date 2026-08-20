import http from './http'

export function getSettings() {
  return http.get('/admin/settings').then((res) => res.data)
}

export function updateSettings(payload) {
  return http.put('/admin/settings', payload).then((res) => res.data)
}

export function sendTestEmail(payload) {
  return http.post('/admin/settings/test-email', payload).then((res) => res.data)
}
