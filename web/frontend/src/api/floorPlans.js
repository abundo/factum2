import http from './http'

export function getFloorPlans(params = {}) {
  return http.get('/dcim/floor-plans', { params }).then((res) => res.data)
}

export function createFloorPlan(payload) {
  return http.post('/dcim/floor-plans', payload).then((res) => res.data)
}

export function renameFloorPlan(id, name) {
  return http.put(`/dcim/floor-plans/${id}`, { name }).then((res) => res.data)
}

export function getFloorPlan(id) {
  return http.get(`/dcim/floor-plans/${id}`).then((res) => res.data)
}

export function saveFloorPlanLayout(id, payload) {
  return http.put(`/dcim/floor-plans/${id}/layout`, payload).then((res) => res.data)
}
