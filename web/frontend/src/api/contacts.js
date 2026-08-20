import http from './http'

export function getContacts() {
  return http.get('/contact').then((res) => res.data)
}
export function createContact(payload) {
  return http.post('/contact', payload).then((res) => res.data)
}
export function updateContact(id, payload) {
  return http.put(`/contact/${id}`, payload).then((res) => res.data)
}
export function getContactCustomers(id) {
  return http.get(`/contact/${id}/customers`).then((res) => res.data)
}
export function setContactCustomers(id, customerIds) {
  return http.put(`/contact/${id}/customers`, { customer_ids: customerIds }).then((res) => res.data)
}
