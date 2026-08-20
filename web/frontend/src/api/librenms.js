import http from './http'

export function getPendingDeletes() {
  return http.get('/librenms/pending-deletes').then((res) => res.data)
}

export function deletePendingNextSync(deviceId) {
  return http.post(`/librenms/pending-deletes/${deviceId}/delete-next-sync`).then((res) => res.data)
}
