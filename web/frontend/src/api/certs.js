import http from './http'

function list(path) {
  return http.get(path).then((res) => res.data)
}
function get(path) {
  return http.get(path).then((res) => res.data)
}
function create(path, payload) {
  return http.post(path, payload).then((res) => res.data)
}
function update(path, payload) {
  return http.put(path, payload).then((res) => res.data)
}
function remove(path) {
  return http.delete(path)
}

export const listCertAccounts = () => list('/certs/accounts')
export const createCertAccount = (payload) => create('/certs/accounts', payload)
export const updateCertAccount = (id, payload) => update(`/certs/accounts/${id}`, payload)
export const deleteCertAccount = (id) => remove(`/certs/accounts/${id}`)

export const listCertChallenges = () => list('/certs/challenges')
export const createCertChallenge = (payload) => create('/certs/challenges', payload)
export const updateCertChallenge = (id, payload) => update(`/certs/challenges/${id}`, payload)
export const deleteCertChallenge = (id) => remove(`/certs/challenges/${id}`)

export const listCertificates = () => list('/certs/certificates')
export const createCertificate = (payload) => create('/certs/certificates', payload)
export const updateCertificate = (id, payload) => update(`/certs/certificates/${id}`, payload)
export const deleteCertificate = (id) => remove(`/certs/certificates/${id}`)
