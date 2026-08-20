import http from './http'

export function getCustomers() {
  return http.get('/customer').then((res) => res.data)
}

export function getCustomer(id) {
  return http.get(`/customer/${id}`).then((res) => res.data)
}

export function createCustomer(payload) {
  return http.post('/customer', payload).then((res) => res.data)
}

export function updateCustomer(id, payload) {
  return http.put(`/customer/${id}`, payload).then((res) => res.data)
}

export function deleteCustomer(id) {
  return http.delete(`/customer/${id}`).then((res) => res.data)
}
