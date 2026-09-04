import http from './http'

export function listDocs() {
  return http.get('/docs').then((res) => res.data)
}

export function getDoc(slug) {
  return http.get(`/docs/${encodeURIComponent(slug)}`).then((res) => res.data)
}
