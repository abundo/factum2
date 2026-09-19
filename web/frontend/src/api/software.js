import http from './http'

export function listSoftware(path = '/') {
  return http.get('/software/files', { params: { path } }).then((res) => res.data)
}

export function mkdirSoftware(path) {
  return http.post('/software/mkdir', { path }).then((res) => res.data)
}

export function moveSoftware(from, to) {
  return http.post('/software/move', { from, to }).then((res) => res.data)
}

export function deleteSoftware(path) {
  return http.delete('/software/files', { params: { path } }).then((res) => res.data)
}

export function uploadSoftware(path, file) {
  const data = new FormData()
  data.append('file', file)
  return http
    .put('/software/files', data, {
      params: { path },
      timeout: 30 * 60 * 1000,
    })
    .then((res) => res.data)
}

export function copySoftware(payload) {
  return http.post('/software/copy', payload, { timeout: 0 }).then((res) => res.data)
}
