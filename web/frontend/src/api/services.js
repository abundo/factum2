import http from './http'

// ServiceID picker always sends this; omit category on the Services page
// so VL/VI/LF/LI stay in the commercial list.
export const SERVICE_ID_PICKER_CATEGORY = 'CN,CI,freetext'

export function getServices(arg) {
  let params
  if (typeof arg === 'number') {
    params = { customer_id: arg }
  } else if (arg && typeof arg === 'object') {
    params = { ...arg }
  }
  return http.get('/service', { params }).then((res) => res.data)
}

export function searchCommercialServices(q) {
  return getServices({ q: q ?? '', category: SERVICE_ID_PICKER_CATEGORY })
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

export function pushService(id) {
  return http.post(`/service/${id}/push`).then((res) => res.data)
}

export function getServiceEndpoints(id) {
  return http.get(`/service/${id}/endpoints`).then((res) => res.data ?? [])
}

export function putServiceEndpoints(id, payload) {
  return http.put(`/service/${id}/endpoints`, payload).then((res) => res.data)
}

// payload is optional - {remove_from_netbox, remove_from_device}, used to
// tear down NetBox objects and/or device config as part of the delete
// (web.ApiServiceDelete/ServiceDeleteRequest). Device login uses Admin →
// Device sync credentials, not the request body. Omitted entirely, this is
// a plain local-only delete.
export function deleteService(id, payload) {
  return http.delete(`/service/${id}`, { data: payload }).then((res) => res.data)
}

export function unrealizeService(id, payload) {
  return http.post(`/service/${id}/unrealize`, payload).then((res) => res.data)
}
