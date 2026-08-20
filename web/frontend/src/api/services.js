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

// Provisions (or re-provisions) an ELINE service's device/interface/VLAN
// endpoints: creates the Netbox L2VPN + subinterfaces + terminations and
// persists the choice on the service - see web/handler_service_eline.go.
export function updateServiceEline(id, payload) {
  return http.put(`/service/${id}/eline`, payload).then((res) => res.data)
}

// Pushes an already-provisioned ELINE service's config out to its endpoint
// device(s) - a separate, explicit step from updateServiceEline above,
// since it mutates live devices. payload is {username, password}; the
// response is {results: [{device, error}]}, one entry per endpoint device -
// see web.ApiServiceElinePush.
export function pushServiceEline(id, payload) {
  return http.post(`/service/${id}/eline/push`, payload).then((res) => res.data)
}

// payload is optional - {remove_from_netbox, remove_from_device, username,
// password}, used to also tear down an ELINE service's NetBox objects
// and/or device config as part of the delete (see
// web.ApiServiceDelete/ServiceDeleteRequest). Omitted entirely, this is a
// plain local-only delete, same as before.
export function deleteService(id, payload) {
  return http.delete(`/service/${id}`, { data: payload }).then((res) => res.data)
}
