import http from './http'

export function getServices(customerId) {
  const params = customerId ? { customer_id: customerId } : undefined
  return http.get('/service', { params }).then((res) => res.data)
}

export function getService(id) {
  return http.get(`/service/${id}`).then((res) => res.data)
}

export function createService(payload) {
  return http.post('/service', payload).then((res) => res.data)
}

export function updateService(id, payload) {
  return http.put(`/service/${id}`, payload).then((res) => res.data)
}

// Sets a service's type/bandwidth/max MAC addresses - the one part of a
// Lime-synced service's record the network GUI can edit, since Lime never
// supplies these and SaveDelivery (internal/lime/lime.go) preserves them
// across future syncs.
export function updateServiceType(id, payload) {
  return http.put(`/service/${id}/type`, payload).then((res) => res.data)
}

// Historical A/B DTO for ELINE endpoints. The GUI uses putServiceEndpoints;
// this adapter remains for API clients.
export function updateServiceEline(id, payload) {
  return http.put(`/service/${id}/eline`, payload).then((res) => res.data)
}

export function pushServiceEline(id, payload) {
  return http.post(`/service/${id}/eline/push`, payload).then((res) => res.data)
}

export function pushService(id, payload) {
  return http.post(`/service/${id}/push`, payload).then((res) => res.data)
}

export function getServiceEndpoints(id) {
  return http.get(`/service/${id}/endpoints`).then((res) => res.data ?? [])
}

export function putServiceEndpoints(id, payload) {
  return http.put(`/service/${id}/endpoints`, payload).then((res) => res.data)
}

// payload is optional - {remove_from_netbox, remove_from_device, username,
// password}, used to also tear down an ELINE service's NetBox objects
// and/or device config as part of the delete (see
// web.ApiServiceDelete/ServiceDeleteRequest). Omitted entirely, this is a
// plain local-only delete, same as before.
export function deleteService(id, payload) {
  return http.delete(`/service/${id}`, { data: payload }).then((res) => res.data)
}
