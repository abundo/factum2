import http from './http'

export function getCustomers() {
  return http.get('/customer').then((res) => res.data)
}

export function getCustomer(id) {
  return http.get(`/customer/${id}`).then((res) => res.data)
}
