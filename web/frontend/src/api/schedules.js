import http from './http'

export function getSchedules() {
  return http.get('/schedules').then((res) => res.data)
}

export function getSchedule(id) {
  return http.get(`/schedules/${id}`).then((res) => res.data)
}

export function createSchedule(payload) {
  return http.post('/schedules', payload).then((res) => res.data)
}

export function updateSchedule(id, payload) {
  return http.put(`/schedules/${id}`, payload).then((res) => res.data)
}

export function deleteSchedule(id) {
  return http.delete(`/schedules/${id}`).then((res) => res.data)
}
